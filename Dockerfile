FROM alpine:latest
LABEL name="dnet" \
      description="D-NET - Dynamic Network Management System" \
      version="1.0" \
      maintainer="cxbdasheng" \
      url="https://github.com/cxbdasheng/dnet" \
      license="MIT"
RUN apk add --no-cache curl grep
WORKDIR /app

COPY zoneinfo /usr/share/zoneinfo
COPY dnet /app/dnet
RUN chmod +x /app/dnet
ENV TZ=Asia/Shanghai \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8
EXPOSE 9877
# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:9877/ || exit 1
# 设置入口点和默认参数
ENTRYPOINT ["/app/dnet"]
CMD ["-l", ":9877", "-f", "300"]
