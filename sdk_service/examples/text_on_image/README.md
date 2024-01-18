# Example IVCAP Service: Image with Gradient Text

This is an extremely simple IVCAP service. It produces an image with a
configurable message super imposed with a gradient fill.

## Build

    make build

## Deploy

Worker:

    export DOCKER_REGISTRY=....
    make DOCKER_REGISTRY=$DOCKER_REGISTRY docker-publish

The service & workflow files should be pushed to the IVCAP cluster via it's `service` API. An alternative solution,
especially for testing, is to setup a tunnel into the Magda registry (see `make -f ../../../k8s/Makefile magda-env`) and then

    make DOCKER_REGISTRY=$DOCKER_REGISTRY magda-deploy

This requires `magda-cli --version` 3.5 and above