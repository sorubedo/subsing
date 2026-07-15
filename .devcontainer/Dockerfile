FROM debian:13

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash-completion \
    build-essential \
    bubblewrap \
    ca-certificates \
    curl \
    git \
    golang-go \
    locales \
    nodejs \
    npm \
    openssh-client \
    pkg-config \
    zip \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN echo "en_US.UTF-8 UTF-8" > /etc/locale.gen \
    && echo "zh_CN.UTF-8 UTF-8" >> /etc/locale.gen \
    && locale-gen

ENV LANG=en_US.UTF-8 \
    LC_CTYPE=en_US.UTF-8

# Go 工作目录和工具安装目录
ENV GOPATH="/go"
ENV GOBIN="/usr/local/bin"
ENV PATH="/root/.local/bin:/go/bin:${PATH}"

RUN mkdir -p \
        "${GOPATH}/bin" \
        "${GOPATH}/pkg/mod" \
        "${GOPATH}/src"

# Go 开发工具：
# gopls       语言服务器
# dlv         调试器
# staticcheck 静态检查
# goimports   格式化并整理 import
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install honnef.co/go/tools/cmd/staticcheck@latest \
    && go install golang.org/x/tools/cmd/goimports@latest \
    && go clean -modcache

# OpenCode
RUN curl -fsSL https://opencode.ai/install | bash

# Codex
RUN npm install -g \
        @openai/codex \
    && npm cache clean --force

# CC Switch
RUN curl -fsSL \
        https://github.com/SaladDay/cc-switch-cli/releases/latest/download/install.sh \
    | bash

RUN cc-switch completions install --shell bash

WORKDIR /workspaces
