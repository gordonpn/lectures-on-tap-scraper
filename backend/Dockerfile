FROM elixir:1.19-alpine

# Install build tools and postgres-client (for pg_isready)
RUN apk add --no-cache build-base inotify-tools postgresql-client

WORKDIR /app

# Install Hex + Rebar
RUN mix local.hex --force && \
    mix local.rebar --force

# Copy mix files first to cache dependencies
COPY mix.exs mix.lock ./
RUN mix deps.get

# Copy the rest of the app
COPY . .

# Run the server
CMD ["sh", "-c", "mix ecto.setup && mix phx.server"]

