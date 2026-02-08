defmodule LecturesOnTap.Notifications.Subscription do
  @moduledoc false

  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:endpoint, :string, []}
  @derive {Jason.Encoder, only: [:endpoint, :topics]}
  schema "push_subscriptions" do
    field(:p256dh, :string)
    field(:auth, :string)
    field(:topics, {:array, :string}, default: [])

    timestamps(type: :utc_datetime)
  end

  def changeset(subscription, attrs) do
    subscription
    |> cast(attrs, [:endpoint, :p256dh, :auth, :topics])
    |> validate_required([:endpoint, :p256dh, :auth])
  end
end
