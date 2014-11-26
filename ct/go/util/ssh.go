package util

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os/user"
	"sync"
	"time"

	"code.google.com/p/go.crypto/ssh"
	"github.com/golang/glog"
)

const (
	KEY_FILE = "id_rsa"
)

type workerResp struct {
	hostname string
	output   string
}

func executeCmd(cmd, hostname string, config *ssh.ClientConfig, timeout time.Duration) (string, error) {
	// Dial up TCP connection to remote machine.
	conn, err := net.Dial("tcp", hostname+":22")
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s. Make sure \"PubkeyAuthentication yes\" is in your sshd_config: %s", hostname, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	// Create new SSH client connection.
	sshConn, sshChan, req, err := ssh.NewClientConn(conn, hostname+":22", config)
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s: %s", hostname, err)
	}
	// Use client connection to create new client.
	client := ssh.NewClient(sshConn, sshChan, req)
	defer client.Close()

	// Client connections can support multiple interactive sessions.
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("Failed to ssh connect to %s: %s", hostname, err)
	}
	defer session.Close()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("Timeout out while running \"%s\" on %s: %s", cmd, hostname, err)
	}
	return stdoutBuf.String(), nil
}

func getKeyFile() (key ssh.Signer, err error) {
	usr, _ := user.Current()
	file := usr.HomeDir + "/.ssh/" + KEY_FILE
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}
	key, err = ssh.ParsePrivateKey(buf)
	if err != nil {
		return
	}
	return
}

// SSH connects to the specified workers and runs the specified command. If the
// command does not complete in the given duration then all remaining workers are
// considered timed out.
func SSH(cmd string, workers []string, timeout time.Duration) (map[string]string, error) {
	glog.Infof("Running \"%s\" on %s with timeout of %s", cmd, workers, timeout)

	// Ensure that the key file exists.
	key, err := getKeyFile()
	if err != nil {
		return nil, fmt.Errorf("Failed to get key file: %s", err)
	}

	// Initialize the structure with the configuration for ssh.
	config := &ssh.ClientConfig{
		User: CT_USER,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
	}

	var wg sync.WaitGroup
	// Will be populated and returned by this function.
	workersWithOutputs := map[string]string{}

	// Kick off a goroutine on all workers.
	for _, hostname := range workers {
		wg.Add(1)
		go func(hostname string) {
			defer wg.Done()
			output, err := executeCmd(cmd, hostname, config, timeout)
			if err != nil {
				glog.Errorf("Could not execute ssh cmd: %s", err)
			}
			workersWithOutputs[hostname] = output
		}(hostname)
	}

	wg.Wait()

	return workersWithOutputs, nil
}
