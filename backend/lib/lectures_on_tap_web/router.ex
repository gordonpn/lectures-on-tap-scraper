defmodule LecturesOnTapWeb.Router do
  use LecturesOnTapWeb, :router

  pipeline :api do
    plug(:accepts, ["json"])
  end

  pipeline :browser do
    plug(:accepts, ["html"])
  end

  pipeline :hub_secret do
    plug(LecturesOnTapWeb.Plugs.SharedSecret)
  end

  scope "/api", LecturesOnTapWeb do
    pipe_through(:api)

    post("/subscribe", SubscriptionController, :subscribe)
    post("/unsubscribe", SubscriptionController, :unsubscribe)
    get("/subscriptions/me", SubscriptionController, :me)
    post("/trigger-self", TriggerController, :trigger_self)
  end

  scope "/api", LecturesOnTapWeb do
    pipe_through([:api, :hub_secret])

    post("/trigger", TriggerController, :trigger)
  end

  scope "/", LecturesOnTapWeb do
    pipe_through(:api)

    get("/healthz", HealthController, :index)
  end

  scope "/", LecturesOnTapWeb do
    pipe_through(:browser)

    get("/*path", SpaController, :index)
  end

  # Enable LiveDashboard and Swoosh mailbox preview in development
  if Application.compile_env(:lectures_on_tap, :dev_routes) do
    # If you want to use the LiveDashboard in production, you should put
    # it behind authentication and allow only admins to access it.
    # If your application does not have an admins-only section yet,
    # you can use Plug.BasicAuth to set up some basic authentication
    # as long as you are also using SSL (which you should anyway).
    import Phoenix.LiveDashboard.Router

    scope "/dev" do
      pipe_through([:fetch_session, :protect_from_forgery])

      live_dashboard("/dashboard", metrics: LecturesOnTapWeb.Telemetry)
      forward("/mailbox", Plug.Swoosh.MailboxPreview)
    end
  end
end
