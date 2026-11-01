ARG ADI_PLATFORM=linux/amd64
FROM --platform=$ADI_PLATFORM python:3.11-slim-bookworm AS adi
ARG PIP_INDEX_URL=https://pypi.org/simple
ARG APT_MIRROR=https://deb.debian.org
RUN sed -i "s|http://deb.debian.org|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev ca-certificates && rm -rf /var/lib/apt/lists/*
COPY deploy/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt
COPY deploy/prepare-runtime.py deploy/verify-encryption.py /tmp/
RUN --mount=type=cache,id=appleservices-public-runtime,target=/runtime-downloads,sharing=locked \
    APPLESERVICES_PUBLIC_CACHE=/runtime-downloads python /tmp/prepare-runtime.py /runtime
COPY nativeadi/assets/transport_fence_linux.c /tmp/transport_fence.c
COPY tools/mac-io-encrypt.c /tmp/mac-io-encrypt.c
RUN gcc -shared -fPIC -O2 /tmp/transport_fence.c -o /runtime/transport_fence.so -ldl \
    && gcc -shared -fPIC -O2 -Wl,-z,noexecstack /tmp/mac-io-encrypt.c /runtime/activation-source-review/encrypt.s -o /runtime/activation-source-review/mac-io-encrypt.so \
    && python /tmp/verify-encryption.py /runtime/activation-source-review/mac-io-encrypt.so

FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN for target in cmd/server cmd/login cmd/trusted-device tools/familyinspect tools/accountsetupcheck tools/nacexchange tools/icloudpurchase tools/localcodehandoff tools/portablecheck; do \
      CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -o /out/$(basename "$target") ./$target || exit 1; \
    done

FROM debian:bookworm-slim AS server
ARG APT_MIRROR=https://deb.debian.org
COPY --from=adi /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN sed -i "s|http://deb.debian.org|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates qemu-user-static python3 python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && find /usr/bin -name 'qemu-*-static' ! -name qemu-x86_64-static -delete \
    && groupadd -g 10001 appleservices && useradd -u 10001 -g 10001 -M -s /usr/sbin/nologin appleservices
COPY deploy/nac-requirements.txt /tmp/nac-requirements.txt
RUN python3 -m venv /opt/nac-runtime \
    && /opt/nac-runtime/bin/python -m pip install --no-cache-dir -r /tmp/nac-requirements.txt \
    && rm /tmp/nac-requirements.txt
WORKDIR /app
COPY --from=adi /usr/local /opt/adi-runtime/usr/local
COPY --from=adi /usr/lib/x86_64-linux-gnu /opt/adi-runtime/usr/lib/x86_64-linux-gnu
COPY --from=adi --chown=10001:10001 /runtime ./.cache
COPY --from=build /out ./bin
COPY deploy/python /usr/local/bin/python
COPY tools/*.py ./tools/
COPY nativeadi/bridge.py ./nativeadi/bridge.py
COPY gsa/apple_root_ca.go gsa/apple_root_ca_g3.pem ./gsa/
COPY LICENSE THIRD_PARTY_NOTICES.md DISCLAIMER.md ./
RUN chmod 755 /usr/local/bin/python && mkdir /data && chown 10001:10001 /data
ENV APPLESERVICES_DIR=/data \
    APPLESERVICES_LISTEN=0.0.0.0:18741 \
    APPLESERVICES_ADI_LIBRARY=/app/.cache/openbubbles-linux/librust_lib_bluebubbles.so \
    APPLESERVICES_ADI_FENCE=/app/.cache/transport_fence.so \
    APPLESERVICES_NAC_PYTHON=/opt/nac-runtime/bin/python \
    XDG_CACHE_HOME=/data/cache \
    PYTHONDONTWRITEBYTECODE=1
USER 10001:10001
EXPOSE 18741
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/app/bin/server", "-healthcheck"]
ENTRYPOINT ["/app/bin/server"]
