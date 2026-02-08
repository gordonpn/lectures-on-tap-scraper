defmodule LecturesOnTapWeb.TriggerController do
  use LecturesOnTapWeb, :controller

  alias LecturesOnTap.Notifications

  def trigger(conn, params) do
    topic = Notifications.normalize_topic(Map.get(params, "topic"))

    case build_payload(params) do
      {:ok, payload} ->
        dry_run? = dry_run?(conn)

        if dry_run? do
          count = Notifications.list_for_topic(topic) |> length()
          json(conn, %{dry_run: true, topic: topic, targets: count})
        else
          {:ok, count} = Notifications.enqueue_topic_delivery(payload, topic)
          json(conn, %{status: "queued", topic: topic, targets: count})
        end

      {:error, _reason} ->
        conn
        |> put_status(:unprocessable_entity)
        |> json(%{error: "invalid_payload"})
    end
  end

  def trigger_self(conn, params) do
    ui_code = Map.get(params, "ui_code")
    endpoint = Map.get(params, "endpoint")

    if valid_ui_code?(ui_code) and is_binary(endpoint) do
      payload =
        Jason.encode!(%{
          title: "Test notification",
          body: "Your Notification Hub is wired up.",
          url: "/"
        })

      {:ok, count} = Notifications.enqueue_single_delivery(payload, endpoint)
      json(conn, %{status: "queued", targets: count})
    else
      conn
      |> put_status(:unauthorized)
      |> json(%{error: "invalid_access_code"})
    end
  end

  defp build_payload(%{"title" => title, "body" => body, "url" => url})
       when is_binary(title) and is_binary(body) and is_binary(url) do
    {:ok, Jason.encode!(%{title: title, body: body, url: url})}
  end

  defp build_payload(_params) do
    {:error, :invalid_payload}
  end

  defp dry_run?(conn) do
    conn.query_params
    |> Map.get("dry_run")
    |> to_string()
    |> String.downcase()
    |> Enum.member?(["true", "1", "yes"])
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
