defmodule LecturesOnTap.Repo.Migrations.CreatePushSubscriptions do
  use Ecto.Migration

  def change do
    create table(:push_subscriptions, primary_key: false) do
      add(:endpoint, :text, primary_key: true)
      add(:p256dh, :text, null: false)
      add(:auth, :text, null: false)
      add(:topics, {:array, :text}, null: false, default: [])

      timestamps(type: :utc_datetime)
    end
  end
end
