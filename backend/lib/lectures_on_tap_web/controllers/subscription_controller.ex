defmodule LecturesOnTapWeb.SubscriptionController do
  use LecturesOnTapWeb, :controller

  plug(LecturesOnTapWeb.Plugs.RateLimit when action in [:subscribe])

  alias LecturesOnTap.Notifications
  alias LecturesOnTap.Notifications.Subscription

  def subscribe(conn, params) do
    ui_code = Map.get(params, "ui_code")

    if valid_ui_code?(ui_code) do
      topic = Notifications.normalize_topic(Map.get(params, "topic"))
      subscription_params = Map.get(params, "subscription", params)

      with {:ok, attrs} <- build_attrs(subscription_params, topic) do
        {existing, _result} = Notifications.upsert_subscription(attrs)
        status = if is_nil(existing), do: :created, else: :ok

        conn
        |> put_status(status)
        |> json(%{status: "active", topics: attrs.topics})
      else
        {:error, reason} ->
          conn
          |> put_status(:unprocessable_entity)
          |> json(%{error: reason})
      end
    else
      conn
      |> put_status(:unauthorized)
      |> json(%{error: "invalid_access_code"})
    end
  end

  def unsubscribe(conn, params) do
    endpoint =
      params
      |> Map.get("endpoint")
      |> fallback_endpoint(params)

    if is_binary(endpoint) do
      Notifications.delete_by_endpoint(endpoint)
      json(conn, %{status: "inactive"})
    else
      conn
      |> put_status(:unprocessable_entity)
      |> json(%{error: "missing_endpoint"})
    end
  end

  def me(conn, params) do
    endpoint = Map.get(params, "endpoint")

    case endpoint && Notifications.get_by_endpoint(endpoint) do
      %Subscription{topics: topics} ->
        json(conn, %{status: "active", topics: topics})

      _ ->
        json(conn, %{status: "inactive", topics: []})
    end
  end

  defp fallback_endpoint(nil, params), do: get_in(params, ["subscription", "endpoint"])
  defp fallback_endpoint(endpoint, _params), do: endpoint

  defp build_attrs(params, topic) do
    endpoint = Map.get(params, "endpoint")
    keys = Map.get(params, "keys", %{})
    p256dh = Map.get(keys, "p256dh") || Map.get(params, "p256dh")
    auth = Map.get(keys, "auth") || Map.get(params, "auth")

    if is_binary(endpoint) and is_binary(p256dh) and is_binary(auth) do
      {:ok, %{endpoint: endpoint, p256dh: p256dh, auth: auth, topics: [topic]}}
    else
      {:error, "invalid_subscription"}
    end
  end

  defp valid_ui_code?(ui_code) do
    expected = Application.get_env(:lectures_on_tap, :hub_ui_code)

    case {expected, ui_code} do
      {exp, code} when is_binary(exp) and is_binary(code) ->
        Plug.Crypto.secure_compare(exp, code)

      _ ->
        false
    end
  end
end
