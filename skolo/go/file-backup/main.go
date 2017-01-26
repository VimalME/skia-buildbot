package main

// file-backup is an executable that backs up a given file to Google storage.
// It is meant to be run on a timer, e.g. daily.

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"time"

	"cloud.google.com/go/storage"
	"go.skia.org/infra/go/auth"
	"go.skia.org/infra/go/common"
	"go.skia.org/infra/go/exec"
	"go.skia.org/infra/go/fileutil"
	"go.skia.org/infra/go/httputils"
	"go.skia.org/infra/go/influxdb"
	"go.skia.org/infra/go/metrics2"
	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/util"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
)

var (
	serviceAccountPath = flag.String("service_account_path", "", "Path to the service account.  Can be empty string to use defaults or project metadata")
	gceBucket          = flag.String("gce_bucket", "skia-backups", "GCS Bucket backups should be stored in")
	gceFolder          = flag.String("gce_folder", "Swarming", "Folder in the bucket that should hold the backup files")
	localFilePath      = flag.String("local_file_path", "", "Where the file is stored locally on disk. Cannot use with remote_file_path")
	remoteFilePath     = flag.String("remote_file_path", "", "Remote location for a file, to be used by remote_copy_command.  E.g. foo@127.0.0.1:/etc/bar.conf Cannot use with local_file_path")
	remoteCopyCommand  = flag.String("remote_copy_command", "scp", "rsync or scp.  The router does not have rsync installed.")

	influxHost     = flag.String("influxdb_host", influxdb.DEFAULT_HOST, "The InfluxDB hostname.")
	influxUser     = flag.String("influxdb_name", influxdb.DEFAULT_USER, "The InfluxDB username.")
	influxPassword = flag.String("influxdb_password", influxdb.DEFAULT_PASSWORD, "The InfluxDB password.")
	influxDatabase = flag.String("influxdb_database", influxdb.DEFAULT_DATABASE, "The InfluxDB database.")
	metricName     = flag.String("metric_name", "rpi-backup", "The metric name that should be used when reporting the size to metrics.")
)

func main() {
	defer common.LogPanic()
	common.InitExternalWithMetrics2("file-backup", influxHost, influxUser, influxPassword, influxDatabase)
	defer metrics2.Flush()
	if *localFilePath == "" && *remoteFilePath == "" {
		sklog.Fatalf("You must specify a file location")
	}
	if *localFilePath != "" && *remoteFilePath != "" {
		sklog.Fatalf("You must specify a local_file_path OR a remote_file_path, not both")
	}

	// We use the plain old http Transport, because the default one doesn't like uploading big files.
	client, err := auth.NewJWTServiceAccountClient("", *serviceAccountPath, &http.Transport{Dial: httputils.DialTimeout}, auth.SCOPE_READ_WRITE, sklog.CLOUD_LOGGING_WRITE_SCOPE)
	if err != nil {
		sklog.Fatalf("Could not setup credentials: %s", err)
	}

	common.StartCloudLoggingWithClient(client, "skolo-rpi-master", "file-backup")

	sklog.Infof("Running backup for %s", *metricName)

	storageClient, err := storage.NewClient(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		sklog.Fatalf("Could not authenticate to GCS: %s", err)
	}

	if *remoteFilePath != "" {
		// If backing up a remote file, copy it here first, then pretend it is a local file.
		dir, err := ioutil.TempDir("", "backups")
		if err != nil {
			sklog.Fatalf("Could not create temp directory %s: %s", dir, err)
		}
		*localFilePath = path.Join(dir, *metricName)
		stdOut := bytes.Buffer{}
		stdErr := bytes.Buffer{}
		// This only works if the remote file's host has the source's SSH public key in
		// $HOME/.ssh/authorized_key
		err = exec.Run(&exec.Command{
			Name:   *remoteCopyCommand,
			Args:   []string{*remoteFilePath, *localFilePath},
			Stdout: &stdOut,
			Stderr: &stdErr,
		})
		sklog.Infof("StdOut of %s command: %s", *remoteCopyCommand, stdOut.String())
		sklog.Infof("StdErr of %s command: %s", *remoteCopyCommand, stdErr.String())
		if err != nil {
			sklog.Fatalf("Could not copy remote file %s: %s", *remoteFilePath, err)
		}
	}

	contents, hash, err := fileutil.ReadAndSha1File(*localFilePath)
	if err != nil {
		sklog.Fatalf("Could not read file %s: %s", *localFilePath, err)
	}

	// We name the file using date and sha1 hash of the file
	day := time.Now().Format("2006-01-02")
	name := fmt.Sprintf("%s/%s-%s.gz", *gceFolder, day, hash)
	w := storageClient.Bucket(*gceBucket).Object(name).NewWriter(context.Background())
	defer util.Close(w)

	w.ObjectAttrs.ContentEncoding = "application/gzip"

	gw := gzip.NewWriter(w)
	defer util.Close(gw)

	sklog.Infof("Uploading %s to gs://%s/%s", *localFilePath, *gceBucket, name)

	// This takes a few minutes for a ~1.3 GB image (which gets compressed to about 400MB)
	if i, err := gw.Write([]byte(contents)); err != nil {
		sklog.Fatalf("Problem writing to GCS.  Only wrote %d/%d bytes: %s", i, len(contents), err)
	} else {
		m := fmt.Sprintf("skolo.%s.backup-size", *metricName)
		metrics2.GetInt64Metric(m, nil).Update(int64(i))
	}

	sklog.Infof("Upload complete")
	sklog.Flush()
}