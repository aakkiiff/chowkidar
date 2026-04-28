# FROM node:24-alpine AS builder

# WORKDIR /app

# COPY package*.json .

# RUN npm ci

# COPY . .

# CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
# ---------- Stage 1: Build ----------
FROM node:24-alpine AS builder

WORKDIR /app

# Install deps
COPY package*.json ./
RUN npm ci

# Copy source
COPY . .

# Build static files
RUN npm run build


# ---------- Stage 2: Serve ----------
FROM nginx:alpine

# Remove default nginx static assets
RUN rm -rf /usr/share/nginx/html/*

# Copy built app from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy custom nginx config (optional but recommended)
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
