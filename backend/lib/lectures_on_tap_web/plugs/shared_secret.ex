defmodule LecturesOnTapWeb.Plugs.SharedSecret do
  @moduledoc false

  import Plug.Conn
  import Phoenix.Controller

  def init(opts), do: opts

  def call(conn, _opts) do
    secret = Application.get_env(:lectures_on_tap, :hub_secret)
    header = conn |> get_req_header("x-hub-secret") |> List.first()

    if valid_secret?(secret, header) do
      conn
    else
      conn
      |> put_status(:unauthorized)
      |> json(%{error: "unauthorized"})
      |> halt()
    end
  end

  defp valid_secret?(secret, header) when is_binary(secret) and is_binary(header) do
    Plug.Crypto.secure_compare(secret, header)
  end

  defp valid_secret?(_, _), do: false
end
