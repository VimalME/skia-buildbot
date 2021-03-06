package main

/*
   Program for automating creation and setup of Swarming bot VMs.
*/

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"go.skia.org/infra/go/auth"
	"go.skia.org/infra/go/common"
	"go.skia.org/infra/go/exec"
	"go.skia.org/infra/go/gce"
	"go.skia.org/infra/go/metadata"
	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/util"
)

const (
	GS_URL_GITCONFIG = "gs://skia-buildbots/artifacts/bots/.gitconfig"
	GS_URL_NETRC     = "gs://skia-buildbots/artifacts/bots/.netrc"

	IP_ADDRESS_TMPL = "104.154.112.%d"
	USER_CHROME_BOT = "chrome-bot"
)

var (
	// Flags.
	instances      = flag.String("instances", "", "Which instances to create/delete, eg. \"2,3-10,22\"")
	create         = flag.Bool("create", false, "Create the instance. Either --create or --delete is required.")
	ct             = flag.Bool("skia-ct", false, "If true, this is a bot in the SkiaCT pool.")
	delete         = flag.Bool("delete", false, "Delete the instance. Either --create or --delete is required.")
	deleteDataDisk = flag.Bool("delete-data-disk", false, "Delete the data disk. Only valid with --delete")
	gpu            = flag.Bool("gpu", false, "Whether or not to add an NVIDIA Tesla k80 GPU on the instance(s)")
	ignoreExists   = flag.Bool("ignore-exists", false, "Do not fail out when creating a resource which already exists or deleting a resource which does not exist.")
	internal       = flag.Bool("internal", false, "Whether or not the bots are internal.")
	skylake        = flag.Bool("skylake", false, "Whether or not the instance(s) should use Intel Skylake CPUs.")
	windows        = flag.Bool("windows", false, "Whether or not the bots run Windows.")
	workdir        = flag.String("workdir", ".", "Working directory.")
)

// Base config for Swarming GCE instances.
func Swarming20170523(name, ipAddress string) *gce.Instance {
	return &gce.Instance{
		BootDisk: &gce.Disk{
			Name:        name,
			SourceImage: "skia-swarming-v3",
			Type:        gce.DISK_TYPE_PERSISTENT_STANDARD,
		},
		DataDisk: &gce.Disk{
			Name:   fmt.Sprintf("%s-data", name),
			SizeGb: 300,
			Type:   gce.DISK_TYPE_PERSISTENT_STANDARD,
		},
		ExternalIpAddress: ipAddress,
		Gpu:               *gpu,
		GSDownloads:       map[string]string{},
		MachineType:       gce.MACHINE_TYPE_STANDARD_16,
		Metadata:          map[string]string{},
		MetadataDownloads: map[string]string{},
		Name:              name,
		Os:                gce.OS_LINUX,
		Scopes: []string{
			auth.SCOPE_FULL_CONTROL,
		},
		Tags: []string{"http-server", "https-server"},
		User: USER_CHROME_BOT,
	}
}

// Configs for Linux GCE instances.
func AddLinuxConfigs(vm *gce.Instance) *gce.Instance {
	vm.GSDownloads["/home/chrome-bot/.gitconfig"] = GS_URL_GITCONFIG
	vm.GSDownloads["/home/chrome-bot/.netrc"] = GS_URL_NETRC

	_, filename, _, _ := runtime.Caller(0)
	dir := path.Dir(filename)
	vm.SetupScript = path.Join(dir, "setup-script-linux.sh")
	return vm
}

// Linux GCE instances.
func LinuxSwarmingBot(num int, ipAddress string) *gce.Instance {
	return AddLinuxConfigs(Swarming20170523(fmt.Sprintf("skia-vm-%03d", num), ipAddress))
}

// Internal Linux GCE instances.
func InternalLinuxSwarmingBot(num int, ipAddress string) *gce.Instance {
	vm := AddLinuxConfigs(Swarming20170523(fmt.Sprintf("skia-i-vm-%03d", num), ipAddress))
	vm.MetadataDownloads["/home/chrome-bot/.gitcookies"] = fmt.Sprintf(metadata.METADATA_URL, "project", "gitcookies_skia-internal_chromium")
	return vm
}

// Skia CT bots.
func SkiaCTBot(num int, ipAddress string) *gce.Instance {
	vm := AddLinuxConfigs(Swarming20170523(fmt.Sprintf("skia-ct-vm-%03d", num), ipAddress))
	vm.DataDisk.SizeGb = 3000
	return vm
}

// Configs for Windows GCE instances.
func AddWinConfigs(vm *gce.Instance, ipAddress, pw, setupScriptPath, startupScriptPath, chromebotScript string) *gce.Instance {
	vm.BootDisk.SizeGb = 300
	vm.BootDisk.SourceImage = "projects/google.com:windows-internal/global/images/windows-server-2008-r2-ent-internal-v20150310"
	vm.BootDisk.Type = gce.DISK_TYPE_PERSISTENT_SSD
	vm.DataDisk = nil
	// Most of the Windows setup, including the gitcookies, occurs in the
	// setup and startup scripts, which also install and schedule the
	// chrome-bot scheduled task script.
	vm.Metadata["chromebot-schtask-ps1"] = chromebotScript
	vm.Os = gce.OS_WINDOWS
	vm.Password = pw
	vm.SetupScript = setupScriptPath
	vm.StartupScript = startupScriptPath
	return vm
}

// Windows GCE instances.
func WinSwarmingBot(num int, ipAddress, pw, setupScriptPath, startupScriptPath, chromebotScript string) *gce.Instance {
	vm := Swarming20170523(fmt.Sprintf("skia-vm-%03d", num), ipAddress)
	return AddWinConfigs(vm, ipAddress, pw, setupScriptPath, startupScriptPath, chromebotScript)
}

// Internal Windows GCE instances.
func InternalWinSwarmingBot(num int, ipAddress, pw, setupScriptPath, startupScriptPath, chromebotScript string) *gce.Instance {
	vm := Swarming20170523(fmt.Sprintf("skia-i-vm-%03d", num), ipAddress)
	return AddWinConfigs(vm, ipAddress, pw, setupScriptPath, startupScriptPath, chromebotScript)
}

// GCE instances with GPUs.
func AddGpuConfigs(vm *gce.Instance) *gce.Instance {
	vm.Gpu = true
	vm.MachineType = gce.MACHINE_TYPE_STANDARD_8            // Max 8 CPUs when using a GPU.
	vm.MaintenancePolicy = gce.MAINTENANCE_POLICY_TERMINATE // Required for GPUs.
	return vm
}

// GCE instances with Skylake CPUs.
func AddSkylakeConfigs(vm *gce.Instance) *gce.Instance {
	vm.MinCpuPlatform = gce.CPU_PLATFORM_SKYLAKE
	return vm
}

// Returns the initial chrome-bot password, plus setup, startup, and
// chrome-bot scripts.
func getWindowsStuff(workdir string) (string, string, string, string, error) {
	pw, err := exec.RunCwd(".", "gsutil", "cat", "gs://skia-buildbots/artifacts/bots/win-chrome-bot.txt")
	if err != nil {
		return "", "", "", "", err
	}
	pw = strings.TrimSpace(pw)

	_, filename, _, _ := runtime.Caller(0)
	repoRoot := path.Dir(path.Dir(path.Dir(path.Dir(filename))))
	setupBytes, err := ioutil.ReadFile(path.Join(repoRoot, "scripts", "win_setup.ps1"))
	if err != nil {
		return "", "", "", "", err
	}
	setupScript := strings.Replace(string(setupBytes), "CHROME_BOT_PASSWORD", pw, -1)
	setupPath := path.Join(workdir, "setup-script.ps1")
	if err := ioutil.WriteFile(setupPath, []byte(setupScript), os.ModePerm); err != nil {
		return "", "", "", "", err
	}

	netrcContents, err := exec.RunCwd(".", "gsutil", "cat", "gs://skia-buildbots/artifacts/bots/.netrc")
	if err != nil {
		return "", "", "", "", err
	}
	setupScript = strings.Replace(setupScript, "INSERTFILE(/tmp/.netrc)", string(netrcContents), -1)

	gitconfigContents, err := exec.RunCwd(".", "gsutil", "cat", "gs://skia-buildbots/artifacts/bots/.gitconfig")
	if err != nil {
		return "", "", "", "", err
	}
	setupScript = strings.Replace(setupScript, "INSERTFILE(/tmp/.gitconfig)", string(gitconfigContents), -1)

	startupBytes, err := ioutil.ReadFile(path.Join(repoRoot, "scripts", "win_startup.ps1"))
	if err != nil {
		return "", "", "", "", err
	}
	startupScript := strings.Replace(string(startupBytes), "CHROME_BOT_PASSWORD", pw, -1)
	startupPath := path.Join(workdir, "startup-script.ps1")
	if err := ioutil.WriteFile(startupPath, []byte(startupScript), os.ModePerm); err != nil {
		return "", "", "", "", err
	}

	// Return the chrome-bot script itself, not its path.
	chromebotBytes, err := ioutil.ReadFile(path.Join(repoRoot, "scripts", "chromebot-schtask.ps1"))
	if err != nil {
		return "", "", "", "", err
	}
	chromebotScript := util.ToDos(string(chromebotBytes))

	return pw, setupPath, startupPath, chromebotScript, nil
}

func main() {
	common.Init()
	defer common.LogPanic()

	// Validation.
	if *create == *delete {
		sklog.Fatal("Please specify --create or --delete, but not both.")
	}

	if *ct && *windows {
		sklog.Fatal("--skia-ct and --windows are mutually exclusive.")
	}
	if *skylake && *gpu {
		sklog.Fatal("--skylake and --gpu are mutually exclusive.")
	}

	instanceNums, err := util.ParseIntSet(*instances)
	if err != nil {
		sklog.Fatal(err)
	}
	if len(instanceNums) == 0 {
		sklog.Fatal("Please specify at least one instance number via --instances.")
	}
	verb := "Creating"
	if *delete {
		verb = "Deleting"
	}
	sklog.Infof("%s instances: %v", verb, instanceNums)

	// Get the absolute workdir.
	wdAbs, err := filepath.Abs(*workdir)
	if err != nil {
		sklog.Fatal(err)
	}

	// Create the GCloud object.
	zone := gce.ZONE_DEFAULT
	if *gpu {
		zone = gce.ZONE_GPU
	} else if *skylake {
		zone = gce.ZONE_SKYLAKE
	}
	g, err := gce.NewGCloud(zone, wdAbs)
	if err != nil {
		sklog.Fatal(err)
	}

	// Read the various Windows scripts.
	var pw, setupScript, startupScript, chromebotScript string
	if *windows {
		pw, setupScript, startupScript, chromebotScript, err = getWindowsStuff(wdAbs)
		if err != nil {
			sklog.Fatal(err)
		}
	}

	// Perform the requested operation.
	group := util.NewNamedErrGroup()
	for _, num := range instanceNums {
		var vm *gce.Instance
		ipAddr := fmt.Sprintf(IP_ADDRESS_TMPL, num)
		if *ct {
			vm = SkiaCTBot(num, ipAddr)
		} else if *windows {
			if *internal {
				vm = InternalWinSwarmingBot(num, ipAddr, pw, setupScript, startupScript, chromebotScript)
			} else {
				vm = WinSwarmingBot(num, ipAddr, pw, setupScript, startupScript, chromebotScript)
			}
		} else {
			if *internal {
				vm = InternalLinuxSwarmingBot(num, ipAddr)
			} else {
				vm = LinuxSwarmingBot(num, ipAddr)
			}
		}
		if *gpu {
			AddGpuConfigs(vm)
		} else if *skylake {
			AddSkylakeConfigs(vm)
		}

		group.Go(vm.Name, func() error {
			if *create {
				if err := g.CreateAndSetup(vm, *ignoreExists); err != nil {
					return err
				}

				if *windows {
					// Reboot. The startup script enabled auto-login as chrome-bot
					// on boot. Reboot in order to run chrome-bot's scheduled task.
					if err := g.Reboot(vm); err != nil {
						return err
					}
				} else {
					// Nothing to do.
				}
			} else {
				return g.Delete(vm, *ignoreExists, *deleteDataDisk)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		sklog.Fatal(err)
	}
}
