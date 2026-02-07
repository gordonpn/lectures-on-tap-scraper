defmodule LecturesOnTap.Repo do
  use Ecto.Repo,
    otp_app: :lectures_on_tap,
    adapter: Ecto.Adapters.Postgres
end
