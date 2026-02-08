defmodule LecturesOnTap.RateLimiter do
  @moduledoc false

  use GenServer

  @table :hub_rate_limiter

  def start_link(_args) do
    GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  end

  def check(ip, limit, window_ms) when is_binary(ip) do
    GenServer.call(__MODULE__, {:check, ip, limit, window_ms})
  end

  @impl true
  def init(_state) do
    table = :ets.new(@table, [:named_table, :set, :protected, read_concurrency: true])
    {:ok, table}
  end

  @impl true
  def handle_call({:check, ip, limit, window_ms}, _from, table) do
    now = System.system_time(:millisecond)
    cutoff = now - window_ms

    recent =
      case :ets.lookup(table, ip) do
        [{^ip, timestamps}] -> Enum.filter(timestamps, &(&1 > cutoff))
        _ -> []
      end

    if length(recent) >= limit do
      {:reply, {:error, :rate_limited}, table}
    else
      :ets.insert(table, {ip, [now | recent]})
      {:reply, :ok, table}
    end
  end
end
