FROM maven:3.8-openjdk-11

ENV JAVA_TOOL_OPTIONS="-XX:+IgnoreUnrecognizedVMOptions --add-exports java.base/jdk.compiler=ALL-UNNAMED"

# Copy the infinispan-operator repository in to /infinispan-operator
RUN mkdir /infinispan-operator
WORKDIR /infinispan-operator
COPY . .

# Create required directories
RUN mkdir /.m2 && mkdir /.kube/

# Add required permissions for OpenShift
RUN chgrp -R 0 /infinispan-operator && \
    chmod -R g=u /infinispan-operator
RUN chgrp -R 0 /.m2 && \
    chmod -R g=u /.m2
RUN chgrp -R 0 /.kube && \
    chmod -R g=u /.kube
RUN chgrp -R 0 /usr && \
    chmod -R g=u /usr


WORKDIR /infinispan-operator/test-integration

CMD ["/bin/bash"]