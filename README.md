# mackerel-plugin-disk

[![Build Status](https://travis-ci.org/y-kuno/mackerel-plugin-disk.svg?branch=master)](https://travis-ci.org/y-kuno/mackerel-plugin-mesos)
![License](https://img.shields.io/github/license/y-kuno/mackerel-plugin-disk.svg)
![Release](https://img.shields.io/github/release/y-kuno/mackerel-plugin-disk.svg)

Disk plugin for mackerel.io agent.  
This repository releases an artifact to Github Releases, which satisfy the format for mkr plugin installer.

## Install

```shell
mkr plugin install y-kuno/mackerel-plugin-disk
```

## Synopsis

```shell
mackerel-plugin-disk [--include-virtual-disk] [--metric-key-prefix=<prefix>]
```

## Example of mackerel-agent.conf

```
[plugin.metrics.disk]
command = "/path/to/mackerel-plugin-disk"
```