FROM maven:3.8-openjdk-11

ENV JAVA_TOOL_OPTIONS="-XX:+IgnoreUnrecognizedVMOptions --add-exports java.base/jdk.compiler=ALL-UNNAMED"

# Copy the infinispan-operator repository in to /infinispan-operator
RUN mkdir /infinispan-operator
WORKDIR /infinispan-operator
COPY . .

# Add required permissions for OpenShift
RUN chgrp -R 0 /infinispan-operator && \
    chmod -R g=u /infinispan-operator

WORKDIR /infinispan-operator/test-integration

CMD ["/bin/bash"]