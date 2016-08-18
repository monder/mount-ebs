## mount-ebs

![GitHub tag](https://img.shields.io/github/tag/monder/mount-ebs.svg?style=flat-square)
[![Build Status](https://img.shields.io/travis/monder/mount-ebs.svg?style=flat-square)](https://travis-ci.org/monder/mount-ebs)

A helper utility initially created for `rkt` to mount `EBS` volumes.

### usage

Mount volume by name:
```bash
$ MOUNTPOINT=`mount-ebs vol-89f4dc0e`
$ echo $MOUNTPOINT
```

Unmount volume:
```bash
$ mount-ebs -u vol-89f4dc0e
```

### behavior

Mount command will try to attach and mount volume. If the volume is already attached, tt will mount it. It tries to attach to the first available device `/dev/sd[f-p]` and will keep retrying until it succeeds or there will be no valid device names left on the host.

Unmount will try to unmount and detach volume. If the mountpoint is in use, the operation is a noop. It is useful for situations when multiple resources want to use the same volume.
