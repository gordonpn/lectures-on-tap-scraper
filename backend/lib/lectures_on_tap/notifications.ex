defmodule LecturesOnTap.Notifications do
  @moduledoc false

  import Ecto.Query, only: [from: 2]

  alias LecturesOnTap.Notifications.Subscription
  alias LecturesOnTap.Repo

  @default_topic "default"

  def normalize_topic(nil), do: @default_topic
  def normalize_topic(""), do: @default_topic
  def normalize_topic(topic) when is_binary(topic), do: topic

  def upsert_subscription(attrs) do
    topics = attrs.topics |> Enum.map(&String.trim/1) |> Enum.reject(&(&1 == "")) |> Enum.uniq()
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    changeset =
      %Subscription{}
      |> Subscription.changeset(%{
        endpoint: attrs.endpoint,
        p256dh: attrs.p256dh,
        auth: attrs.auth,
        topics: topics
      })

    existing = Repo.get(Subscription, attrs.endpoint)

    result =
      Repo.insert(changeset,
        conflict_target: :endpoint,
        on_conflict: [
          set: [p256dh: attrs.p256dh, auth: attrs.auth, topics: topics, updated_at: now]
        ]
      )

    {existing, result}
  end

  def delete_by_endpoint(endpoint) when is_binary(endpoint) do
    Repo.delete_all(from(s in Subscription, where: s.endpoint == ^endpoint))
  end

  def get_by_endpoint(endpoint) when is_binary(endpoint) do
    Repo.get(Subscription, endpoint)
  end

  def list_for_topic(topic) do
    topic = normalize_topic(topic)
    Repo.all(from(s in Subscription, where: ^topic in s.topics))
  end

  def enqueue_topic_delivery(payload, topic) do
    subscriptions = list_for_topic(topic)
    spawn_delivery(subscriptions, payload)
  end

  def enqueue_single_delivery(payload, endpoint) do
    case get_by_endpoint(endpoint) do
      nil -> {:ok, 0}
      subscription -> spawn_delivery([subscription], payload)
    end
  end

  defp spawn_delivery(subscriptions, payload) do
    Task.Supervisor.start_child(LecturesOnTap.Notifications.TaskSupervisor, fn ->
      deliver_now(subscriptions, payload)
    end)

    {:ok, length(subscriptions)}
  end

  defp deliver_now(subscriptions, payload) do
    config = Application.get_env(:lectures_on_tap, __MODULE__, [])
    max_concurrency = Keyword.get(config, :max_concurrency, 10)

    # Bounded concurrency keeps outbound push calls under control.
    Task.Supervisor.async_stream_nolink(
      LecturesOnTap.Notifications.TaskSupervisor,
      subscriptions,
      fn subscription ->
        LecturesOnTap.Notifications.WebPushClient.send_notification(subscription, payload)
      end,
      max_concurrency: max_concurrency,
      timeout: 30_000
    )
    |> Stream.run()
  end
end
