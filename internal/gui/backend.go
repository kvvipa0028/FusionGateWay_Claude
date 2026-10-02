package gui

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/update"
)

// served is the gateway this process serves, nil while another magpie
// has it.
var served atomic.Pointer[gateway.Server]

// backendCtx ends the gateway this process serves (stopServing).
var backendCtx, endBackend = context.WithCancel(context.Background())

// serving is the gateway this process serves, until it has finished.
var serving sync.WaitGroup

// stopServing stops this process's gateway taking requests and returns
// once those in flight have finished.
func stopServing() {
	endBackend()
	serving.Wait()
}

// startBackend starts what serves the page and the agents: the gateway,
// unless another magpie has it (then that one serves and this one only
// shows its status, and gw is nil — until that one is gone: a magpie left
// running from before an update, a magpie serve in a terminal), and the
// model lists kept warm.
func startBackend() (gw *gateway.Server) {
	gateway.Window = true // the routing this process serves is shown on its page
	gw = serveGateway()
	go watchGateway()
	// Model lists are fetched, never compiled in: whatever the agents can see
	// comes from the models.dev catalog plus each vendor's own /models answer.
	// Keep both halves warm without making the user click anything.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if catalog.Stale() {
			if err := catalog.Sync(ctx); err != nil {
				log.Println("catalog:", err)
			}
		}
		cancel()
		// A signed-in agent's list exists only at the vendor; fill it in the
		// first time so the picker never shows a stale snapshot.
		provider.FetchNew(20 * time.Second)
		// lists an older magpie wrote into agents' files, without what
		// it has learnt since (context windows, providers added)
		agent.SyncCatalog()
		// and fetched again each day it stays open, for new models' prices
		catalog.KeepFresh()
	}()
	return gw
}

var gatewayWatch = 15 * time.Second

// watchGateway takes the gateway up once the magpie that had it is gone.
func watchGateway() {
	for {
		time.Sleep(gatewayWatch)
		if served.Load() == nil && !gateway.Running() {
			serveGateway()
		}
	}
}

// serveGateway starts the gateway here when no magpie has it: the one
// started, or nil.
func serveGateway() *gateway.Server {
	// handing over, the one there is this one's predecessor, which lets go
	// once this one listens beside it
	if !gateway.Handover {
		if o := gateway.ServedBy(); o.Running {
			if update.Newer(gateway.Version, o.Version) {
				log.Printf("gateway: magpie %s serves %s, older than this one (%s); agents' requests go through it until it quits", o.Version, gateway.URL(), gateway.Version)
			}
			return nil
		}
	}
	gw := gateway.New()
	served.Store(gw)
	serving.Add(1)
	go func() {
		defer serving.Done()
		if err := gw.ListenAndServe(backendCtx); err != nil {
			log.Println("gateway:", err)
			served.CompareAndSwap(gw, nil) // another took the port first
		}
	}()
	return gw
}
