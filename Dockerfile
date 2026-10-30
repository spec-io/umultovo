FROM node:22-alpine AS web-build
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json ./
COPY web/ ./web/
RUN npm run build

FROM golang:1.24-alpine AS go-build
WORKDIR /build
COPY Source/go.mod ./Source/go.mod
COPY Source/main.go ./Source/main.go
RUN cd Source && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rcn-tool .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 rcn
WORKDIR /app
COPY index.html ./
COPY --from=web-build /build/page.js ./page.js
COPY Data/ ./Data/
COPY --from=go-build /out/rcn-tool ./rcn-tool
RUN mkdir -p Data/RCN Data/CSV Source \
    && ln -s Data/RCN RCN \
    && ln -s Data/CSV CSV \
    && chown -R rcn:rcn /app
USER rcn
WORKDIR /app/Source
ENV RCN_CONTAINER=1
EXPOSE 8080
ENTRYPOINT ["/app/rcn-tool"]
CMD ["serve", "--addr", "0.0.0.0:8080"]
