defmodule LecturesOnTapWeb.Plugs.RateLimit do
  @moduledoc false

  import Plug.Conn
  import Phoenix.Controller

  alias LecturesOnTap.RateLimiter

  def init(opts), do: opts

  def call(conn, _opts) do
    limits = Application.get_env(:lectures_on_tap, :hub_limits, [])
    limit = Keyword.get(limits, :subscribe_rate_limit, 5)
    window_ms = Keyword.get(limits, :subscribe_rate_window_ms, 60_000)
    ip = client_ip(conn)

    case RateLimiter.check(ip, limit, window_ms) do
      :ok ->
        conn

      {:error, :rate_limited} ->
        conn
        |> put_status(:too_many_requests)
        |> json(%{error: "rate_limited"})
        |> halt()
    end
  end

  defp client_ip(conn) do
    forwarded =
      conn
      |> get_req_header("x-forwarded-for")
      |> List.first()

    if is_binary(forwarded) and forwarded != "" do
      forwarded
      |> String.split(",")
      |> List.first()
      |> String.trim()
    else
      conn.remote_ip
      |> :inet.ntoa()
      |> to_string()
    end
  end
end
