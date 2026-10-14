FROM node:22.22.1-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends git bash ca-certificates coreutils python3 && rm -rf /var/lib/apt/lists/*
RUN git config --system user.name "Romp" && git config --system user.email "romp@users.noreply.github.com" && git config --system commit.gpgsign false
RUN mkdir -p /workspace
WORKDIR /workspace
CMD ["sleep", "infinity"]
