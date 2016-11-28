package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	awsSession "github.com/aws/aws-sdk-go/aws/session"
	awsEC2 "github.com/aws/aws-sdk-go/service/ec2"
)

func MountVolume(id string, format string) (string, error) {
	mountpoint := mountpointForId(id)
	fmt.Fprintf(os.Stderr, "mount volume %s to %s\n", id, mountpoint)

	device, err := attachVolume(id)
	if err != nil {
		return "", err
	}

	err = mountVolume(device, mountpointForId(id))
	if err != nil {
		if format != "" {
			if out, err := exec.Command("mkfs", "-t", format, device).CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "formatting device %s to %s failed: %v\n%v", device, format, err, string(out))
			}
			err = mountVolume(device, mountpointForId(id))
		}
		if err != nil {
			// Make sure to detach the volume
			detachVolume(id)
			return "", err
		}
	}

	return mountpoint, nil

}

func UnmountVolume(id string) error {
	fmt.Fprintf(os.Stderr, "unmount volume %s\n", id)

	err := unmountVolume(mountpointForId(id))
	if err != nil {
		return err
	}

	err = detachVolume(id)
	if err != nil {
		return err
	}
	return nil
}

// Private

func mountpointForId(id string) string {
	return fmt.Sprintf("/mnt/ebs/%s", id)
}

func getInstance() (*awsEC2.EC2, string, error) {
	session := awsSession.New()
	metadata := ec2metadata.New(session)
	region, err := metadata.Region()
	if err != nil {
		return nil, "", err
	}
	instanceId, err := metadata.GetMetadata("instance-id")
	if err != nil {
		return nil, "", err
	}
	return awsEC2.New(session, &aws.Config{Region: aws.String(region)}), instanceId, nil
}

func getFreeDeviceName(skip int) (string, error) {
	for _, c := range "fghijklmnop" {
		dev := "/dev/sd" + string(c)
		altdev := "/dev/xvd" + string(c)
		if _, err := os.Lstat(dev); err == nil {
			continue
		}
		if _, err := os.Lstat(altdev); err == nil {
			continue
		}
		if skip > 0 {
			skip -= 1
			continue
		}
		return dev, nil
	}
	return "", fmt.Errorf("no device names available for attachment: /dev/sd[f-p] taken")
}

func getAttachedVolumeDevice(id string) (string, error) {
	ec2, instanceId, err := getInstance()
	if err != nil {
		return "", err
	}
	info, err := ec2.DescribeVolumes(&awsEC2.DescribeVolumesInput{
		VolumeIds: []*string{aws.String(id)},
	})
	if err != nil {
		return "", err
	}
	if len(info.Volumes[0].Attachments) == 1 &&
		*info.Volumes[0].Attachments[0].State == awsEC2.VolumeAttachmentStateAttached &&
		*info.Volumes[0].Attachments[0].InstanceId == instanceId {
		re := regexp.MustCompile("/dev/(xv|s)d([a-z])")
		res := re.FindStringSubmatch(*info.Volumes[0].Attachments[0].Device)
		if len(res) != 3 {
			return "", fmt.Errorf("unable to find mount device for %s", id)
		}
		if _, err := os.Lstat("/dev/sd" + res[2]); err == nil {
			return "/dev/sd" + res[2], nil
		}
		if _, err := os.Lstat("/dev/xvd" + res[2]); err == nil {
			return "/dev/xvd" + res[2], nil
		}
		return "", fmt.Errorf("volume attached, but no device found")
	}
	// TODO handle detach if attached to another instance
	return "", fmt.Errorf("volume %s is not attached", id)
}

func attachVolume(id string) (string, error) {

	fmt.Fprintf(os.Stderr, "attaching volume %s...\n", id)

	device, err := getAttachedVolumeDevice(id)
	if err == nil {
		fmt.Fprintf(os.Stderr, "volume %s attached as %s...\n", id, device)
		return device, nil
	}

	ec2, instanceId, err := getInstance()
	if err != nil {
		return "", err
	}

	for tries := 0; tries < 25; tries++ {
		device, err = getFreeDeviceName(tries)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "using device %s\n", device)

		err = detachVolume(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			//TODO not attached return "", err
		}

		_, err = ec2.AttachVolume(&awsEC2.AttachVolumeInput{
			Device:     aws.String(device),
			InstanceId: aws.String(instanceId),
			VolumeId:   aws.String(id),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			continue
		}

		fmt.Fprintf(os.Stderr, "waiting for volume to attach...\n")

		for tries := 0; tries < 3; tries++ {
			device, err = getAttachedVolumeDevice(id)
			if err == nil {
				return device, nil
			}
			time.Sleep(5 * time.Second)
		}
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	return "", fmt.Errorf("volume failed to attach")

}

func mountVolume(device string, mountpoint string) error {
	fmt.Fprintf(os.Stderr, "mounting device %s to %s...\n", device, mountpoint)

	err := os.MkdirAll(mountpoint, os.ModeDir|0700)
	if err != nil {
		return err
	}

	mounted, err := isMounted(mountpoint)
	if err != nil {
		return err
	}
	if mounted {
		return nil
	}

	if out, err := exec.Command("mount", device, mountpoint).CombinedOutput(); err != nil {
		return fmt.Errorf("mounting device %s to %s failed: %v\n%v", device, mountpoint, err, string(out))
	}

	return nil
}

func isMounted(mountpoint string) (bool, error) {
	stat, err := os.Stat(mountpoint)
	if err != nil || !stat.IsDir() {
		return false, fmt.Errorf("mountpoint %v is not a directory", mountpoint)
	}
	parentStat, err := os.Stat(filepath.Join(mountpoint, ".."))
	if err == nil {
		mountpointDev := stat.Sys().(*syscall.Stat_t).Dev
		parentDev := parentStat.Sys().(*syscall.Stat_t).Dev
		if mountpointDev != parentDev {
			// Something is mounted there.
			// TODO check if its the correct device?
			return true, nil
		}
	}
	return false, err
}

func detachVolume(id string) error {
	//TODO check if already detached
	// status == available
	fmt.Fprintf(os.Stderr, "detaching volume %s...\n", id)
	ec2, instanceId, err := getInstance()
	if err != nil {
		return err
	}
	_, err = ec2.DetachVolume(&awsEC2.DetachVolumeInput{
		InstanceId: aws.String(instanceId),
		VolumeId:   aws.String(id),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "waiting for volume %s detachment...\n", id)

	err = ec2.WaitUntilVolumeAvailable(&awsEC2.DescribeVolumesInput{
		VolumeIds: []*string{aws.String(id)},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "volume %s detached\n", id)
	return nil
}

func unmountVolume(mountpoint string) error {
	fmt.Fprintf(os.Stderr, "unmounting %s...\n", mountpoint)

	mounted, err := isMounted(mountpoint)
	if err != nil {
		return err
	}
	if !mounted {
		fmt.Fprintf(os.Stderr, "path %s is not mounted\n", mountpoint)
		return nil
	}

	if err := exec.Command("lsof", mountpoint).Run(); err == nil {
		return fmt.Errorf("mountpoint %s is in use", mountpoint)
	}

	if err := syscall.Unmount(mountpoint, 0); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "removing directory %s...\n", mountpoint)
	if err := os.Remove(mountpoint); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s unmounted and removed\n", mountpoint)
	return nil
}
