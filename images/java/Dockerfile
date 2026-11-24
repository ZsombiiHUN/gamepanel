# GamePanel's Java runtime for Minecraft servers and proxies.
#   docker build --build-arg JAVA_VERSION=25 -t ghcr.io/zsombiihun/gamepanel-java:25 images/java
# Eclipse Temurin JRE (HotSpot): the fastest free JVM for Minecraft in community
# benchmarks; OpenJ9 uses less memory but runs Minecraft markedly slower.
ARG JAVA_VERSION=25
# Not pinned by digest on purpose: one Dockerfile builds every Java version, and
# the weekly rebuild should pick up the newest Temurin patch and Ubuntu fixes.
FROM eclipse-temurin:${JAVA_VERSION}-jre-noble

# curl and jq are for gp-install; fontconfig for plugins that render maps or images.
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl jq ca-certificates tzdata fontconfig \
 && rm -rf /var/lib/apt/lists/*

COPY --chmod=0755 gp-java gp-install /usr/local/bin/

# The daemon runs the container as the owner of the data directory, a user
# with no passwd entry; give Java a writable home all the same.
ENV HOME=/home/container TZ=UTC
WORKDIR /home/container
CMD ["gp-java"]
