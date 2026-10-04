# syntax=docker/dockerfile:1
# Jenkins inbound agent: JDK 21 (from the base image), Maven, Go and the buildlens CLI.
# Build context: the repository root (the CLI is built from cmd/buildlens). Versions checked 2026-10-04.

FROM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS cli
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/buildlens ./cmd/buildlens

# Debian 13 base, OpenJDK 21.0.12.1, git 2.47.3, user jenkins (uid 1000).
FROM jenkins/inbound-agent:3391.va_37fa_a_305d6d-4-jdk21@sha256:c5d50ca09a7de45999983c69e0527499ca667efe00a85cf1ce5ed968b5b21719

USER root

# Maven 3.9.16 (newest 3.x; 4.0.0 is still a release candidate), SHA-512 from Maven Central.
ARG MAVEN_VERSION=3.9.16
ARG MAVEN_SHA512=831a8591fe20c8243b1dbe7d71e3244f31d1665b0804b2e825e38cbbe5ce0cafb8338851f90780735568773e0a6cd07bbec107cda0b896b008b861075358b6f6
RUN curl -fsSLo /tmp/maven.tar.gz "https://repo1.maven.org/maven2/org/apache/maven/apache-maven/${MAVEN_VERSION}/apache-maven-${MAVEN_VERSION}-bin.tar.gz" \
 && echo "${MAVEN_SHA512}  /tmp/maven.tar.gz" | sha512sum -c - \
 && tar -xzf /tmp/maven.tar.gz -C /opt \
 && ln -s "/opt/apache-maven-${MAVEN_VERSION}/bin/mvn" /usr/local/bin/mvn \
 && rm /tmp/maven.tar.gz

# Go 1.27.1 (the project's pinned toolchain), SHA-256 from https://go.dev/dl/?mode=json. Used from P5 (dogfooding).
ARG GO_VERSION=1.27.1
ARG GO_SHA256=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
RUN curl -fsSLo /tmp/go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
 && echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/go.tar.gz -C /usr/local \
 && rm /tmp/go.tar.gz
ENV PATH=/usr/local/go/bin:$PATH

COPY --from=cli /out/buildlens /usr/local/bin/buildlens
COPY jenkins/agent/entrypoint.sh /usr/local/bin/buildlens-agent-entrypoint
# /buildlens-agent is the mount point of the volume shared with the controller. Whichever container mounts the
# empty volume first copies its directory's owner into it, so both images must give it to jenkins (uid 1000).
RUN chmod 755 /usr/local/bin/buildlens-agent-entrypoint \
 && mkdir -p /home/jenkins/.m2 /buildlens-agent \
 && chown jenkins:jenkins /home/jenkins/.m2 /buildlens-agent \
 && chmod 750 /buildlens-agent

USER jenkins
ENTRYPOINT ["/usr/local/bin/buildlens-agent-entrypoint"]
