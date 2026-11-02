FROM node:22-alpine AS base
WORKDIR /app
# pnpm version comes from "packageManager" in package.json
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .

# Test stage: docker build --target test ./frontend
FROM base AS test
RUN pnpm test

FROM base AS build
RUN pnpm build

# Production runtime: static files only, served by nginx as a non-root user.
FROM nginxinc/nginx-unprivileged:1.29-alpine AS prod
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
