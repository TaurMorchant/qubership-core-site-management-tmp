package utils

import (
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"os"
	"os/signal"
	"syscall"
)

var log = logging.GetLogger("utils")

func RegisterShutdownHook(hook func(exitCode int)) {
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, syscall.SIGINT, syscall.SIGTERM)

		sig := <-sigint
		log.Info("OS signal '%s' received, starting shutdown", sig.String())

		exitCode := 0
		switch sig {
		case syscall.SIGINT:
			exitCode = 130
		case syscall.SIGTERM:
			exitCode = 143
		default:
			log.Info("No code remapping rule for code: %d, return exit code 0", sig)
		}
		hook(exitCode)
	}()
}
