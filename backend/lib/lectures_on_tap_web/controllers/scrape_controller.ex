defmodule LecturesOnTapWeb.ScrapeController do
  use LecturesOnTapWeb, :controller

  def latest(conn, _params) do
    case LecturesOnTap.Redis.command(["GET", "scraper:last_run"]) do
      {:ok, nil} ->
        json(conn, %{data: nil})

      {:ok, payload} when is_binary(payload) ->
        case Jason.decode(payload) do
          {:ok, decoded} -> json(conn, decoded)
          _ -> json(conn, %{raw: payload})
        end

      {:error, :not_configured} ->
        conn
        |> put_status(:service_unavailable)
        |> json(%{error: "redis_not_configured"})

      {:error, _reason} ->
        conn
        |> put_status(:service_unavailable)
        |> json(%{error: "redis_unavailable"})
    end
  end
end
