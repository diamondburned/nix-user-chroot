package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var usage = "Usage: " + os.Args[0] + " <nixpath> <command>\n"

func write(file, content string) {
	f, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		log.Fatalln("Failed to open "+file+":", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(content)); err != nil {
		log.Fatalln("Failed to write to "+file+":", err)
	}
}

func main() {
	log.SetFlags(0)

	if len(os.Args) < 3 {
		log.Fatalln(usage)
	}

	rootdir, err := ioutil.TempDir(os.TempDir(), "nix")
	if err != nil {
		log.Fatalln("Failed to make tempdir:", err)
	}

	nixdir, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatalln("Failed to get absolute path:", err)
	}

	if err := unix.Unshare(unix.CLONE_NEWNS | unix.CLONE_NEWUSER); err != nil {
		log.Fatalln("Error at unshare():", err)
	}

	d, err := os.Open("/")
	if err != nil {
		log.Fatalln("Failed to open /:", err)
	}
	defer d.Close()

	dirs, err := d.Readdir(-1)
	if err != nil {
		log.Fatalln("Failed to read directory /:", err)
	}

	for _, dir := range dirs {
		// Skip nix/
		if dir.Name() == "nix" {
			continue
		}

		// Check if directory.
		if !dir.IsDir() {
			continue
		}

		local := filepath.Join("/", dir.Name())
		mount := filepath.Join(rootdir, dir.Name())

		if err := os.Mkdir(mount, dir.Mode()&^unix.S_IFMT); err != nil {
			log.Println("Failed to mkdir:", err)
			continue
		}

		if err := unix.Mount(local, mount, "none", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			log.Printf("Failed to bind mount %s to %s: %v\n", local, mount, err)
			continue
		}
	}

	s, err := os.Stat(nixdir)
	if err != nil {
		log.Fatalln("Failed to stat Nix directory:", err)
	}

	nixmount := filepath.Join(rootdir, "nix")

	if err := os.Mkdir(nixmount, s.Mode()&^unix.S_IFMT); err != nil {
		log.Fatalln("FFailed to mount Nix:", err)
	}

	// fixes issue #1 where writing to /proc/self/gid_map fails
	// see user_namespaces(7) for more documentation
	setgroups, err := os.OpenFile("/proc/self/setgroups", os.O_WRONLY, 0)
	if err != nil {
		log.Fatalln("Failed to open setgroups:", err)
	}
	setgroups.Write([]byte("deny"))
	setgroups.Close()

	uid := os.Getuid()
	gid := os.Getgid()

	// map the original uid/gid in the new ns
	write("/proc/self/uid_map", fmt.Sprintf("%d %d 1", uid, uid))
	write("/proc/self/gid_map", fmt.Sprintf("%d %d 1", gid, gid))

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalln("Failed to get working directory:", err)
	}

	// Change the directory to our root before entering.
	if err := os.Chdir("/"); err != nil {
		log.Fatalln("Failed to cd /:", err)
	}

	// chroot inside.
	if err := unix.Chroot(rootdir); err != nil {
		log.Fatalln("Failed to chroot:", err)
	}

	// then cd inside the chrooted directory:
	if err := os.Chdir(cwd); err != nil {
		log.Fatalln("Failed to chdir to inside chroot:", err)
	}

	os.Setenv("NIX_CONF_DIR", "/nix/etc/nix")

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmdErr := cmd.Run()

	// in case the process exited out
	if cmd.ProcessState != nil {
		os.Exit(cmd.ProcessState.ExitCode())
	}

	// in case the process didn't even run
	if cmdErr != nil {
		log.Fatalln("cmd error:", err)
	}
}
