package main

import (
	launcher "github.com/infinispan/infinispan-operator/launcher"
	// +kubebuilder:scaffold:imports
)

func main() {
	launcher.Launch(launcher.Parameters{})
}
