FROM node:22.14.0-bookworm-slim
WORKDIR /app
COPY server.mjs ./server.mjs
USER node
EXPOSE 8080
CMD ["node", "server.mjs"]
