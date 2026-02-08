defmodule LecturesOnTap.Notifications.WebPushClient do
  @moduledoc false

  alias LecturesOnTap.Notifications
  require Logger

  def send_notification(subscription, payload) do
    config = Application.get_env(:lectures_on_tap, Notifications, [])
    max_retries = Keyword.get(config, :max_retries, 3)
    retry_base_backoff_ms = Keyword.get(config, :retry_base_backoff_ms, 400)
    ttl_seconds = Keyword.get(config, :ttl_seconds, 60 * 60 * 24 * 14)

    do_send(subscription, payload, ttl_seconds, max_retries, retry_base_backoff_ms, 0)
  end

  defp do_send(subscription, payload, ttl_seconds, max_retries, retry_base_backoff_ms, attempt) do
    case build_request(subscription, payload, ttl_seconds) do
      {:ok, endpoint, headers, body} ->
        Logger.debug(
          "push_send start endpoint=#{redact_endpoint(endpoint)} payload_bytes=#{byte_size(body)}"
        )

        case Req.post(endpoint, headers: headers, body: body, retry: false) do
          {:ok, %Req.Response{status: status} = response} when status in 200..299 ->
            Logger.info(
              ~s|push_send ok endpoint=#{redact_endpoint(endpoint)} status=#{status} apns_id=#{header_value(response, "apns-id") || "n/a"}|
            )

            :ok

          {:ok, %Req.Response{status: 410}} ->
            Logger.warning("push_send gone endpoint=#{redact_endpoint(endpoint)} status=410")
            Notifications.delete_by_endpoint(subscription.endpoint)
            {:error, :gone}

          {:ok, %Req.Response{status: status} = response}
          when status in 500..599 and attempt < max_retries ->
            Logger.warning(
              ~s|push_send retryable endpoint=#{redact_endpoint(endpoint)} status=#{status} attempt=#{attempt} apns_id=#{header_value(response, "apns-id") || "n/a"}|
            )

            Process.sleep(trunc(retry_base_backoff_ms * :math.pow(2, attempt)))

            do_send(
              subscription,
              payload,
              ttl_seconds,
              max_retries,
              retry_base_backoff_ms,
              attempt + 1
            )

          {:ok, %Req.Response{status: status} = response} ->
            Logger.error(
              ~s|push_send failed endpoint=#{redact_endpoint(endpoint)} status=#{status} apns_id=#{header_value(response, "apns-id") || "n/a"} body=#{response_body_preview(response)}|
            )

            {:error, {:http_error, status}}

          {:error, _error} when attempt < max_retries ->
            Logger.warning(
              "push_send error retry endpoint=#{redact_endpoint(endpoint)} attempt=#{attempt}"
            )

            Process.sleep(trunc(retry_base_backoff_ms * :math.pow(2, attempt)))

            do_send(
              subscription,
              payload,
              ttl_seconds,
              max_retries,
              retry_base_backoff_ms,
              attempt + 1
            )

          {:error, error} ->
            Logger.error(
              "push_send error endpoint=#{redact_endpoint(endpoint)} reason=#{inspect(error)}"
            )

            {:error, error}
        end

      {:error, _} = error ->
        Logger.error(
          "push_send build_request_failed endpoint=#{redact_endpoint(subscription.endpoint)} reason=#{inspect(error)}"
        )

        error
    end
  end

  defp build_request(subscription, payload, ttl_seconds) do
    subscription_payload = %{
      endpoint: subscription.endpoint,
      keys: %{p256dh: subscription.p256dh, auth: subscription.auth}
    }

    encryption_result = WebPushEncryption.encrypt(payload, subscription_payload)

    encrypted =
      case encryption_result do
        {:ok, payload_map} -> payload_map
        payload_map when is_map(payload_map) -> payload_map
        {:error, _} = error -> error
      end

    with encrypted when is_map(encrypted) <- encrypted,
         {:ok, vapid_headers} <- vapid_headers(subscription.endpoint, ttl_seconds) do
      body = Map.fetch!(encrypted, :ciphertext)
      salt = Map.fetch!(encrypted, :salt)
      server_public_key = Map.fetch!(encrypted, :server_public_key)

      headers =
        vapid_headers ++
          [
            {"Urgency", "high"},
            {"Topic", "lectures-on-tap"},
            {"Content-Encoding", "aesgcm"},
            {"Encryption", "salt=#{Base.url_encode64(salt, padding: false)}"},
            {"Crypto-Key",
             "dh=#{Base.url_encode64(server_public_key, padding: false)};p256ecdsa=#{vapid_public_key()}"}
          ]

      {:ok, subscription.endpoint, headers, body}
    else
      {:error, _} = error -> error
    end
  end

  # VAPID tokens are a short-lived JWT signed by our P-256 private key.
  defp vapid_headers(endpoint, ttl_seconds) do
    config = Application.get_env(:lectures_on_tap, Notifications, [])
    vapid_public_key = Keyword.get(config, :vapid_public_key)
    vapid_private_key = Keyword.get(config, :vapid_private_key)
    vapid_subject = Keyword.get(config, :vapid_subject)

    if is_nil(vapid_public_key) or is_nil(vapid_private_key) do
      Logger.error("push_send missing_vapid_keys")
      {:error, :missing_vapid_keys}
    else
      aud = endpoint_origin(endpoint)
      jwt = build_vapid_jwt(vapid_public_key, vapid_private_key, aud, vapid_subject)

      {:ok,
       [
         {"TTL", Integer.to_string(ttl_seconds)},
         {"Authorization", "vapid t=#{jwt}, k=#{vapid_public_key}"}
       ]}
    end
  end

  defp vapid_public_key do
    Application.get_env(:lectures_on_tap, Notifications, [])
    |> Keyword.get(:vapid_public_key)
  end

  defp build_vapid_jwt(vapid_public_key, vapid_private_key, aud, subject) do
    exp = System.system_time(:second) + 60 * 60 * 12
    claims = %{"aud" => aud, "exp" => exp, "sub" => subject || aud}
    jwk = jwk_from_vapid_keys(vapid_public_key, vapid_private_key)

    {_, token} =
      jwk
      |> JOSE.JWT.sign(%{"alg" => "ES256"}, claims)
      |> JOSE.JWS.compact()

    token
  end

  defp jwk_from_vapid_keys(vapid_public_key, vapid_private_key) do
    {:ok, public_raw} = Base.url_decode64(vapid_public_key, padding: false)
    {:ok, private_raw} = Base.url_decode64(vapid_private_key, padding: false)

    <<4, x::binary-size(32), y::binary-size(32)>> = public_raw

    jwk_map = %{
      "kty" => "EC",
      "crv" => "P-256",
      "x" => Base.url_encode64(x, padding: false),
      "y" => Base.url_encode64(y, padding: false),
      "d" => Base.url_encode64(private_raw, padding: false)
    }

    JOSE.JWK.from_map(jwk_map)
  end

  defp endpoint_origin(endpoint) do
    uri = URI.parse(endpoint)
    base = "#{uri.scheme}://#{uri.host}"

    if is_nil(uri.port) or uri.port in [80, 443] do
      base
    else
      base <> ":#{uri.port}"
    end
  end

  defp redact_endpoint(endpoint) when is_binary(endpoint) do
    case URI.parse(endpoint) do
      %URI{scheme: scheme, host: host} when is_binary(scheme) and is_binary(host) ->
        "#{scheme}://#{host}"

      _ ->
        "unknown"
    end
  end

  defp response_body_preview(%Req.Response{body: body}) do
    preview =
      case body do
        binary when is_binary(binary) -> binary
        other -> inspect(other)
      end

    String.slice(preview, 0, 300)
  end

  defp header_value(%Req.Response{headers: headers}, key) when is_binary(key) do
    headers
    |> Enum.find_value(fn
      {^key, value} ->
        value

      {header_key, value} when is_binary(header_key) ->
        if String.downcase(header_key) == key, do: value, else: nil

      _ ->
        nil
    end)
  end
end
