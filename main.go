package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	unmount := flag.Bool("u", false, "unmount")
	format := flag.String("f", "", "format")
	flag.Parse()

	volumeId := flag.Arg(0)
	if volumeId == "" {
		fmt.Fprintf(os.Stderr, "No volume id specified\n")
		os.Exit(1)
	}

	if *unmount {
		UnmountVolume(volumeId)
	} else {
		mountpoint, err := MountVolume(volumeId, *format)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(2)
		}
		fmt.Println(mountpoint)
	}
}
