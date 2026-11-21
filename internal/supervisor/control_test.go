package supervisor

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// k37: a natural exit and a StopService that are ready together reached the
// loop's select, which picks one at random. Picking the exit fired OnExit for
// a stop the user had asked for, which in headless mode shuts the whole
// launcher down. The stop has to win: no OnExit, no restart, and the status
// of a stopped service, as when select picks the stop first.
//
// The seam: exitTaken holds the loop between taking the exit and acting on
// it, and the control channel is buffered for this test, so the test can see
// the stop waiting there before it lets the loop go on. On the real,
// unbuffered channel a waiting sender cannot be seen.
func TestStopWinsOverAnExitReadyWithIt(t *testing.T) {
	var log lineLog
	sup := New(log.log)
	spec, _ := runsSpec(t, "f", 0) // exits 1 at once
	spec.Restart = "never"         // so an exit acted on fires OnExit
	exits := make(chan Exit, 2)
	spec.OnExit = func(e Exit) { exits <- e }
	if err := sup.AddService(spec); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	svc := sup.services["svc"]
	svc.ctrl = make(chan ctrlMsg, 1)
	stopped := make(chan error, 1)
	var once sync.Once
	sup.exitTaken = func() {
		once.Do(func() {
			go func() {
				_, err := sup.StopService("svc")
				stopped <- err
			}()
			for len(svc.ctrl) == 0 {
				runtime.Gosched()
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("StopService: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stop waiting with the exit was never answered")
	}
	// The loop logs acting on the exit before it answers the stop, so this
	// check needs no wait; OnExit runs in a goroutine of its own and gets one.
	if out := log.String(); strings.Contains(out, "no automatic restart") || !strings.Contains(out, "stopped (manual)") {
		t.Fatalf("the exit was acted on instead of the stop:\n%s", out)
	}
	select {
	case e := <-exits:
		t.Fatalf("OnExit fired (%+v) for a stop the user asked for", e)
	case <-time.After(200 * time.Millisecond):
	}
	// Design choice: the same status as when select picks the stop first.
	st := sup.Status()
	if len(st) != 1 || st[0].Unresponsive || st[0].Desired || st[0].Running || st[0].LastExit != "" {
		t.Fatalf("status after the stop = %+v, want stopped, not desired, no lastExit: the run ended as the stop's", st)
	}
}

// startBlockedInPreStart runs a service whose PreStart blocks until the test
// ends, with a short control timeout. It returns once the loop is inside
// PreStart when autostart is set; otherwise PreStart runs on the first start.
func startBlockedInPreStart(t *testing.T, autostart bool) (*Supervisor, <-chan struct{}) {
	t.Helper()
	requireBin(t, "sleep")
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	sup := New(nil)
	sup.ctrlTimeout = 500 * time.Millisecond
	if err := sup.AddService(ServiceSpec{
		Name:      "svc",
		Command:   "sleep",
		Args:      []string{"30"},
		Autostart: autostart,
		Restart:   "never",
		PreStart: func(Start) {
			entered <- struct{}{}
			<-release
		},
	}); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { close(release); cancel(); wg.Wait() })
	if autostart {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the first start never reached PreStart")
		}
	}
	return sup, entered
}

// k37: send() guarded handing the message over with a timeout, then waited
// for the reply with a bare receive. A loop that took the message and then
// blocked — here in the PreStart of the start it was asked for — hung the
// caller, an MCP handler, forever. The call has to give up after the control
// timeout and say that the request was taken and is still being carried out.
func TestControlCallTimesOutWaitingForTheReply(t *testing.T) {
	sup, entered := startBlockedInPreStart(t, false)
	done := make(chan error, 1)
	go func() {
		_, err := sup.StartService("svc")
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("StartService still waits for the reply of a loop blocked in PreStart")
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the start StartService asked for never reached PreStart")
	}
	if err == nil {
		t.Fatal("StartService reported success while its start was still in PreStart")
	}
	msg := err.Error()
	for _, want := range []string{"took the request", "is still carrying it out", "do not repeat it", "busy with its pre-start"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want %q: the request was taken and goes on, busy with its pre-start", msg, want)
		}
	}
}

// k37 review: a taken request that outlasts the control timeout goes on. It
// is sent to the agent as an error, so the text has to say that the request is
// still being carried out and is not to be repeated: "did not answer" read as
// hung, and a repeated ue_restart waits behind the first and then restarts
// the editor that one just spawned. This is the case without a PreStart.
func TestTakenRequestTimeoutSaysItGoesOn(t *testing.T) {
	svc := &service{spec: ServiceSpec{Name: "ue"}}
	msg := svc.timeoutError(true, 10*time.Second).Error()
	for _, want := range []string{`"ue" took the request`, "is still carrying it out after 10s", "do not repeat it", "check status"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	}
	if strings.Contains(msg, "pre-start") {
		t.Fatalf("error = %q names a pre-start the service is not in", msg)
	}
}

// k37: PreStart runs on the control loop (the editor's shells out to kill the
// crash reporter and clears Saved/Crashes), so a call that comes meanwhile
// cannot hand its message over. It used to be told the service "is not
// responding", which reads as hung. It has to say the service is busy with
// its pre-start, and that nothing was done.
func TestControlCallNamesABusyPreStart(t *testing.T) {
	sup, _ := startBlockedInPreStart(t, true)
	_, err := sup.StopService("svc")
	if err == nil {
		t.Fatal("StopService succeeded while the loop was blocked in PreStart")
	}
	msg := err.Error()
	if strings.Contains(msg, "not responding") || !strings.Contains(msg, "busy with its pre-start") {
		t.Fatalf("error = %q, want it to name the pre-start the service is busy with", msg)
	}
	if !strings.Contains(msg, "nothing was done") {
		t.Fatalf("error = %q, want it to say the request was not carried out", msg)
	}
}

// k37: AddService overwrote the map entry but appended the name to the order
// again, so Start ran two loops on one service. A duplicate name is refused,
// and the service registered first stays.
func TestAddServiceRefusesADuplicateName(t *testing.T) {
	sup := New(nil)
	if err := sup.AddService(ServiceSpec{Name: "ue", Command: "first"}); err != nil {
		t.Fatalf("first AddService: %v", err)
	}
	err := sup.AddService(ServiceSpec{Name: "ue", Command: "second"})
	if err == nil || !strings.Contains(err.Error(), `"ue"`) {
		t.Fatalf("second AddService = %v, want an error naming the duplicate", err)
	}
	if names := sup.ServiceNames(); len(names) != 1 {
		t.Fatalf("ServiceNames = %v, want the name once", names)
	}
	if got := sup.services["ue"].spec.Command; got != "first" {
		t.Fatalf("service command = %q, want the first registration kept", got)
	}
}
