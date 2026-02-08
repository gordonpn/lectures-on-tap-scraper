defmodule LecturesOnTapWeb.SpaController do
  use LecturesOnTapWeb, :controller

  def index(conn, _params) do
    path = Application.app_dir(:lectures_on_tap, "priv/static/index.html")
    send_file(conn, 200, path)
  end
end
