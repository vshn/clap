[![Build](https://img.shields.io/github/actions/workflow/status/vshn/clap/build.yaml?branch=main)](https://github.com/vshn/clap/actions?query=workflow%3ABuild)
[![Test](https://img.shields.io/github/actions/workflow/status/vshn/clap/test.yaml?branch=main&label=test)](https://github.com/vshn/clap/actions?query=workflow%3ATest)
![Go version](https://img.shields.io/github/go-mod/go-version/vshn/clap)
[![Version](https://img.shields.io/github/v/release/vshn/clap)](https://github.com/vshn/clap/releases)

# clap

Claim Lifecycle and Provisioning (CLAP) for AppSlap.

A Kubernetes operator that discovers claim CRDs and manages their lifecycle and provisioning.

> [!NOTE]
> This project is an early work in progress. APIs and behaviour are subject to change.

## Building

See `make help` for the full list of targets.

* `make build`: Build the manager binary
* `make test`: Run tests
* `make docker-build`: Build the container image
