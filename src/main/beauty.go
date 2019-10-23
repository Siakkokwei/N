package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	log "github.com/nulastudio/NetCoreBeauty/src/log"
	manager "github.com/nulastudio/NetCoreBeauty/src/manager"
	util "github.com/nulastudio/NetCoreBeauty/src/util"

	"github.com/bitly/go-simplejson"
)

const (
	errorLevel  string = "Error"  // log errors only
	detailLevel string = "Detail" // log useful infos
	infoLevel   string = "Info"   // log everything
)

var workingDir, _ = os.Getwd()
var runtimeCompatibilityJSON *simplejson.Json
var runtimeSupportedJSON *simplejson.Json

var gitcdn string
var nopatch bool
var loglevel string
var beautyDir string
var libsDir = "runtimes"

func main() {
	Umask()

	initCLI()

	// 设置CDN
	manager.GitCDN = gitcdn

	// 设置LogLevel
	log.DefaultLogger.LogLevel = map[string]log.LogLevel{
		errorLevel:  log.Error,
		detailLevel: log.Detail,
		infoLevel:   log.Info,
	}[loglevel]
	manager.Logger.LogLevel = log.DefaultLogger.LogLevel

	log.LogInfo("running ncbeauty...")

	beautyCheck := path.Join(beautyDir, "NetCoreBeauty")

	// 检查是否已beauty
	if util.PathExists(beautyCheck) {
		log.LogDetail("already beauty. Enjoy it!")
		return
	}

	// 必须检查
	manager.CheckRunConfigJSON()

	// fix runtimeconfig.json
	runtimeConfigs := manager.FindRuntimeConfigJSON(beautyDir)
	if len(runtimeConfigs) != 0 {
		for _, runtimeConfig := range runtimeConfigs {
			log.LogDetail(fmt.Sprintf("fixing %s", runtimeConfig))
			manager.FixRuntimeConfig(runtimeConfig, libsDir)
			log.LogDetail(fmt.Sprintf("%s fixed", runtimeConfig))
		}
	} else {
		log.LogDetail(fmt.Sprintf("no runtimeconfig.json found in %s", beautyDir))
		log.LogDetail("skipping")
		os.Exit(0)
	}

	// fix deps.json
	dependencies := manager.FindDepsJSON(beautyDir)
	if len(dependencies) != 0 {
		for _, deps := range dependencies {
			log.LogDetail(fmt.Sprintf("fixing %s", deps))
			deps = strings.ReplaceAll(deps, "\\", "/")
			mainProgram := strings.Replace(path.Base(deps), ".deps.json", "", -1)
			depsFiles, fxrVersion, rid := manager.FixDeps(deps)
			// patch
			if nopatch {
				fmt.Println("hostfxr patch has been disable, skipped")
			} else if fxrVersion != "" && rid != "" {
				patch(fxrVersion, rid)
			} else {
				log.LogError(errors.New("incomplete fxr info, skipping patch"), false)
			}
			if len(depsFiles) == 0 {
				continue
			}
			log.LogDetail(fmt.Sprintf("%s fixed", deps))
			log.LogInfo("moving runtime...")
			moved := moveDeps(depsFiles, mainProgram)
			log.LogDetail(fmt.Sprintf("%d of %d runtime files moved", moved, len(depsFiles)))
		}
	} else {
		log.LogDetail(fmt.Sprintf("no deps.json found in %s", beautyDir))
		log.LogDetail("skipping")
		os.Exit(0)
	}

	// 写入beauty标记
	if err := ioutil.WriteFile(beautyCheck, nil, 0666); err != nil {
		log.LogPanic(fmt.Errorf("beauty sign failed: %s", err.Error()), 1)
	}

	log.LogDetail("ncbeauty done. Enjoy it!")
}

func initCLI() {
	flag.CommandLine = flag.NewFlagSet("ncbeauty", flag.ContinueOnError)
	flag.CommandLine.Usage = usage
	flag.CommandLine.SetOutput(os.Stdout)
	flag.StringVar(&gitcdn, "gitcdn", "https://github.com/nulastudio/HostFXRPatcher", `specify a HostFXRPatcher mirror repo if you have troble in connecting github.
RECOMMEND https://gitee.com/liesauer/HostFXRPatcher for mainland china users.
`)
	flag.StringVar(&loglevel, "loglevel", "Error", `log level. valid values: Error/Detail/Info
Error: Log errors only.
Detail: Log useful infos.
Info: Log everything.
`)
	flag.BoolVar(&nopatch, "nopatch", false, `disable hostfxr patch.
DO NOT DISABLE!!!
hostfxr patch fixes https://github.com/nulastudio/NetCoreBeauty/issues/1`)

	flag.Parse()

	args := make([]string, 0)

	// 内置的坑爹flag不对空格做忽略处理
	for _, arg := range flag.Args() {
		if arg != "" && arg != " " {
			args = append(args, arg)
		}
	}
	argv := len(args)

	// 必需参数检查
	if argv == 0 {
		usage()
		os.Exit(0)
	}
	beautyDir = args[0]

	if len(args) >= 2 {
		libsDir = flag.Arg(1)
	}

	beautyDir = strings.Trim(beautyDir, `"`)
	libsDir = strings.Trim(libsDir, `"`)
	absDir, err := filepath.Abs(beautyDir)
	if err != nil {
		log.LogPanic(fmt.Errorf("invalid beautyDir: %s", err.Error()), 1)
	}
	beautyDir = absDir

	// logLevel检查
	if loglevel != errorLevel && loglevel != detailLevel && loglevel != infoLevel {
		loglevel = errorLevel
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("ncbeauty [--<gitcdn>] [--<loglevel=Error|Detail|Info>] [--<nopatch=True|False>] <beautyDir> [<libsDir>]")
	flag.PrintDefaults()
}

func patch(fxrVersion string, rid string) bool {
	log.LogDetail("patching hostfxr...")

	crid := manager.FindCompatibleRID(rid)
	fxrName := manager.GetHostFXRNameByRID(rid)
	if crid == "" {
		log.LogPanic(fmt.Errorf("cannot find a compatible rid for %s", rid), 1)
	}

	log.LogDetail(fmt.Sprintf("using compatible rid %s for %s", crid, rid))
	rid = crid

	if manager.GetLocalArtifactsVersion(fxrVersion, rid) == "" {
		log.LogDetail(fmt.Sprintf("downloading patched hostfxr: %s/%s", fxrVersion, rid))

		if !manager.DownloadArtifact(fxrVersion, rid) || !manager.WriteLocalArtifactsVersion(fxrVersion, rid, manager.GetOnlineArtifactsVersion()) {
			log.LogPanic(errors.New("download patch failed"), 1)
		}
	}

	absFxrName := path.Join(beautyDir, fxrName)
	absFxrBakName := absFxrName + ".bak"
	log.LogInfo(fmt.Sprintf("backuping fxr to %s\n", absFxrBakName))

	if util.PathExists(absFxrBakName) {
		log.LogDetail("fxr backup found, skipped")
	} else {
		if _, err := util.CopyFile(absFxrName, absFxrBakName); err != nil {
			log.LogError(fmt.Errorf("backup failed: %s", err.Error()), false)
			return false
		}
	}

	success := manager.CopyArtifactTo(fxrVersion, rid, beautyDir)
	if success {
		log.LogInfo("patch succeeded")
	} else {
		fmt.Println("patch failed")
	}

	return success
}

func moveDeps(depsFiles []string, mainProgram string) int {
	moved := 0
	for _, depsFile := range depsFiles {
		if strings.Contains(depsFile, mainProgram) ||
			strings.Contains(depsFile, "apphost") ||
			strings.Contains(depsFile, "hostfxr") ||
			strings.Contains(depsFile, "hostpolicy") {
			// 将其加一，不然每次看到日志的文件移动数少3会造成疑惑
			moved++
			continue
		}

		absDepsFile := path.Join(beautyDir, depsFile)
		absDesFile := path.Join(beautyDir, libsDir, depsFile)
		oldPath := path.Dir(absDepsFile)
		newPath := path.Dir(absDesFile)
		if util.PathExists(absDepsFile) {
			if !util.EnsureDirExists(newPath, 0777) {
				log.LogError(fmt.Errorf("%s is not writeable", newPath), false)
			}
			if err := os.Rename(absDepsFile, absDesFile); err == nil {
				moved++
			}

			// NOTE: 需要移动附带的pdb、xml文件吗？
			// NOTE: pdb、xml文件是跟随程序还是跟随依赖dll？
			fileName := strings.TrimSuffix(path.Base(depsFile), path.Ext(depsFile))
			extFiles := []string{".pdb", ".xml"}
			for _, extFile := range extFiles {
				oldFile := path.Join(oldPath, fileName+extFile)
				newFile := path.Join(newPath, fileName+extFile)
				if util.PathExists(oldFile) {
					os.Rename(oldFile, newFile)
				}
			}
			dir, _ := ioutil.ReadDir(oldPath)
			if len(dir) == 0 {
				os.Remove(oldPath)
			}
		}
	}
	return moved
}