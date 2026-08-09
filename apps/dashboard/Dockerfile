# syntax=docker/dockerfile:1
# Build the SPA, then serve it with a minimal nginx that also reverse-proxies
# /api and /healthz to the Pulse API container. This nginx is Pulse's OWN,
# isolated instance — it never touches the user's existing Nginx.
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json ./
# Plain npm (no corepack/pnpm bootstrap, which needs an extra network fetch).
RUN npm install --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
