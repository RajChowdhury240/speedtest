package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

// Stage identifies a measurement phase the UI cares about.
type Stage string

const (
	StageInit     Stage = "init"
	StageUser     Stage = "user"
	StageServers  Stage = "servers"
	StagePing     Stage = "ping"
	StageDownload Stage = "download"
	StageUpload   Stage = "upload"
	StageDone     Stage = "done"
)

// Update is streamed from the engine to the UI.
type Update struct {
	Stage Stage

	// User info (StageUser)
	IP  string
	ISP string
	Lat string
	Lon string

	// Server info (StageServers)
	ServerName    string
	ServerSponsor string
	ServerCountry string
	ServerHost    string
	ServerID      string
	DistanceKM    float64

	// Live measurements
	PingMS     float64
	JitterMS   float64
	DownloadMB float64 // Mbps
	UploadMB   float64 // Mbps

	Err error
}

// Run launches the speedtest pipeline and streams updates over the returned channel.
// Channel closes when the run finishes or errors.
func Run(ctx context.Context) <-chan Update {
	out := make(chan Update, 32)

	go func() {
		defer close(out)

		send := func(u Update) {
			select {
			case out <- u:
			case <-ctx.Done():
			}
		}

		send(Update{Stage: StageInit})

		client := speedtest.New()

		user, err := client.FetchUserInfoContext(ctx)
		if err != nil {
			send(Update{Stage: StageUser, Err: fmt.Errorf("fetch user info: %w", err)})
			return
		}
		send(Update{
			Stage: StageUser,
			IP:    user.IP,
			ISP:   user.Isp,
			Lat:   user.Lat,
			Lon:   user.Lon,
		})

		servers, err := client.FetchServerListContext(ctx)
		if err != nil {
			send(Update{Stage: StageServers, Err: fmt.Errorf("fetch servers: %w", err)})
			return
		}
		targets, err := servers.FindServer(nil)
		if err != nil || len(targets) == 0 {
			send(Update{Stage: StageServers, Err: fmt.Errorf("no server: %w", err)})
			return
		}
		s := targets[0]
		send(Update{
			Stage:         StageServers,
			ServerName:    s.Name,
			ServerSponsor: s.Sponsor,
			ServerCountry: s.Country,
			ServerHost:    s.Host,
			ServerID:      s.ID,
			DistanceKM:    s.Distance,
		})

		// Ping (latency + jitter); callback streams each probe.
		if err := s.PingTestContext(ctx, func(latency time.Duration) {
			send(Update{Stage: StagePing, PingMS: durMS(latency)})
		}); err != nil {
			send(Update{Stage: StagePing, Err: err})
			return
		}
		send(Update{
			Stage:    StagePing,
			PingMS:   durMS(s.Latency),
			JitterMS: durMS(s.Jitter),
		})

		// Download — live samples via callback.
		s.Context.SetCallbackDownload(func(rate speedtest.ByteRate) {
			send(Update{Stage: StageDownload, DownloadMB: rate.Mbps()})
		})
		if err := s.DownloadTestContext(ctx); err != nil {
			send(Update{Stage: StageDownload, Err: err})
			return
		}
		send(Update{Stage: StageDownload, DownloadMB: s.DLSpeed.Mbps()})

		// Upload
		s.Context.SetCallbackUpload(func(rate speedtest.ByteRate) {
			send(Update{Stage: StageUpload, UploadMB: rate.Mbps()})
		})
		if err := s.UploadTestContext(ctx); err != nil {
			send(Update{Stage: StageUpload, Err: err})
			return
		}
		send(Update{Stage: StageUpload, UploadMB: s.ULSpeed.Mbps()})

		send(Update{
			Stage:      StageDone,
			PingMS:     durMS(s.Latency),
			JitterMS:   durMS(s.Jitter),
			DownloadMB: s.DLSpeed.Mbps(),
			UploadMB:   s.ULSpeed.Mbps(),
		})
	}()

	return out
}

func durMS(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}
