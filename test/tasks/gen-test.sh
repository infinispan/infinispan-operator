#!/usr/bin/env bash

mvn clean package
kubectl create configmap external-libs-config --from-file task01/target/task01-1.0.0.jar --from-file task02/target/task02-1.0.0.zip --from-file task03/target/task03-1.0.0.tar.gz --from-file index.html -o yaml --dry-run=client > ../e2e/utils/data/external-libs-config.yaml
