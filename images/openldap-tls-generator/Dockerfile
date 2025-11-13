FROM registry.access.redhat.com/ubi9/ubi:9.4

ARG KUBECTL_VERSION=v1.30.0

RUN dnf install -y --nodocs \
      ca-certificates \
      openssl \
      tar \
      gzip \
      shadow-utils \
    && dnf clean all \
    && rm -rf /var/cache/dnf

RUN curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
    && chmod 0755 /usr/local/bin/kubectl \
    && ln -s /usr/local/bin/kubectl /usr/local/bin/oc

RUN useradd --uid 10001 --no-create-home --shell /sbin/nologin tls \
    && install -d -o 10001 -g 10001 /work

WORKDIR /work

USER 10001

ENTRYPOINT ["/bin/bash"]
