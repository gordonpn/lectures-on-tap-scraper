defmodule LecturesOnTap.Redis do
  @moduledoc false

  def child_spec(_args) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [[]]},
      type: :worker,
      restart: :permanent,
      shutdown: 500
    }
  end

  def start_link(_args) do
    case Application.get_env(:lectures_on_tap, :redis_url) do
      nil -> :ignore
      redis_url -> Redix.start_link(redis_url, name: __MODULE__)
    end
  end

  def command(command) do
    case Process.whereis(__MODULE__) do
      nil -> {:error, :not_configured}
      _pid -> Redix.command(__MODULE__, command)
    end
  end
end
