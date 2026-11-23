package supervisor

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	// k188: nor OnRunEnd, which runs on the loop before the stop is answered.
	var runEnds atomic.Int32
	spec.OnRunEnd = func(RunEnd) { runEnds.Add(1) }
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
	if n := runEnds.Load(); n != 0 {
		t.Fatalf("OnRunEnd fired %d times for a run the stop took", n)
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

// callResult is what a control call returned.
type callResult struct {
	st  ServiceStatus
	err error
}

// runWithQueuedCall runs spec under sup until the test ends or parent is
// done, with call made to wait in the service's control channel the first
// time the loop reaches seam (&sup.exitTaken or &sup.backoffTaken): the loop
// is held there until the message sits in the channel, which is buffered for
// this, as in TestStopWinsOverAnExitReadyWithIt. It returns the call's result
// once the loop has answered it, and the OnExit events.
func runWithQueuedCall(t *testing.T, parent context.Context, sup *Supervisor, spec ServiceSpec, seam *func(), call func() (ServiceStatus, error)) (callResult, <-chan Exit) {
	t.Helper()
	exits := make(chan Exit, 4)
	spec.OnExit = func(e Exit) { exits <- e }
	if err := sup.AddService(spec); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	svc := sup.services[spec.Name]
	svc.ctrl = make(chan ctrlMsg, 1)
	res := make(chan callResult, 1)
	var once sync.Once
	*seam = func() {
		once.Do(func() {
			go func() {
				st, err := call()
				res <- callResult{st, err}
			}()
			for len(svc.ctrl) == 0 {
				runtime.Gosched()
			}
		})
	}
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("the queued call failed: %v", r.err)
		}
		return r, exits
	case <-time.After(10 * time.Second):
		t.Fatal("the call waiting at the seam was never answered")
		return callResult{}, nil
	}
}

// controlCall returns the named control call ("stop", "start", "restart" or
// "status") on the service "svc" of sup.
func controlCall(sup *Supervisor, kind string) func() (ServiceStatus, error) {
	switch kind {
	case "stop":
		return func() (ServiceStatus, error) { return sup.StopService("svc") }
	case "start":
		return func() (ServiceStatus, error) { return sup.StartService("svc") }
	case "restart":
		return func() (ServiceStatus, error) { return sup.RestartService("svc") }
	default:
		return func() (ServiceStatus, error) {
			st := sup.Status()
			if len(st) != 1 || st[0].Unresponsive {
				return ServiceStatus{}, fmt.Errorf("status = %+v", st)
			}
			return st[0], nil
		}
	}
}

// k179: a control message ready together with a due restart timer reached
// the loop's select, which picks one at random. Picking the timer started the
// process and only then carried out the message: a stop killed an editor that
// had just been spawned, a start found it running and did nothing, so it did
// not replace the pending restart (k153), and a restart spawned it twice. The
// message is taken first and carried out instead of the timer's start; a
// status is answered after that start, as on the next turn.
//
// Run 1 fails at once and the policy arms its restart; the call waits when
// the timer comes due. Every later run keeps running until the test ends.
// The delayed first start (a start delay) is the same timer: a stop waiting
// when it comes due cancels it, and a start is a plain start there, since no
// restart is pending.
func TestControlCallWaitingWithADueRestart(t *testing.T) {
	cases := []struct {
		name, call          string
		startDelay          bool   // the timer is the delayed first start
		automatic, replaces string // PreStart flags of every start
		running, desired    bool
		lastExit            string
	}{
		{"stop", "stop", false, "[false]", "[false]", false, false, "exit 1"},
		{"start", "start", false, "[false false]", "[false true]", true, true, ""},
		{"restart", "restart", false, "[false false]", "[false true]", true, true, ""},
		{"status", "status", false, "[false true]", "[false false]", true, true, "exit 1"},
		{"stop at the delayed first start", "stop", true, "[]", "[]", false, false, ""},
		{"start at the delayed first start", "start", true, "[false]", "[false]", true, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pre preStarts
			sup := New(nil)
			spec, _ := runsSpec(t, "fs", time.Minute)
			if c.startDelay {
				spec, _ = runsSpec(t, "s", time.Minute)
				spec.StartDelay = 10 * time.Millisecond
			}
			spec.PreStart = pre.record
			r, _ := runWithQueuedCall(t, context.Background(), sup, spec, &sup.backoffTaken, controlCall(sup, c.call))
			// PreStart runs on the loop before the call is answered.
			if got := pre.String(); got != c.automatic {
				t.Fatalf("PreStart Automatic = %s, want %s with a %s waiting when the timer came due", got, c.automatic, c.call)
			}
			if got := pre.replacesRestart(); got != c.replaces {
				t.Fatalf("PreStart ReplacesRestart = %s, want %s", got, c.replaces)
			}
			if r.st.Running != c.running || r.st.Desired != c.desired || r.st.LastExit != c.lastExit {
				t.Fatalf("%s answered %+v, want running %v, desired %v, lastExit %q", c.call, r.st, c.running, c.desired, c.lastExit)
			}
		})
	}
}

// k179, from the k37 review: a start or restart waiting when the process
// ended on its own was carried out after the exit, but OnExit fired first.
// For a headless agent, OnExit is what shuts the launcher down, so a console
// "r agent" that raced the agent's exit ended the launcher. The call is the
// user's action: OnExit does not fire and the call is carried out.
//
// Design choice (k179): the exit still counts as an exit. Under restart:
// never the start after it is the first start after a failure that is not
// restarted (AfterGiveUp, so the crash evidence is kept, k128); where the
// policy restarts it, the start takes that restart's place (ReplacesRestart,
// k153). The call clears lastExit and the counts as it always does.
//
// Only a start or restart takes the exit over: a status waiting at it is
// answered, and the exit is judged as any exit, so OnExit fires.
func TestStartOrRestartWinsOverAnExitReadyWithIt(t *testing.T) {
	cases := []struct {
		name, call, restart              string
		afterGiveUp, replaces, automatic string // PreStart flags of every start
		running                          bool
		lastExit                         string
		onExit                           bool
	}{
		{"start, restart never", "start", "never", "[false true]", "[false false]", "[false false]", true, "", false},
		{"restart, restart never", "restart", "never", "[false true]", "[false false]", "[false false]", true, "", false},
		{"start, the exit arms a restart", "start", "on-failure", "[false false]", "[false true]", "[false false]", true, "", false},
		{"status, restart never", "status", "never", "[false]", "[false]", "[false]", false, "exit 1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pre preStarts
			sup := New(nil)
			spec, _ := runsSpec(t, "fs", time.Minute) // run 1 exits 1 at once
			spec.Restart = c.restart
			spec.RestartDelay = time.Hour
			spec.PreStart = pre.record
			r, exits := runWithQueuedCall(t, context.Background(), sup, spec, &sup.exitTaken, controlCall(sup, c.call))
			if r.st.Running != c.running || !r.st.Desired || r.st.Restarts != 0 || r.st.LastExit != c.lastExit {
				t.Fatalf("%s answered %+v, want running %v, desired, restarts 0, lastExit %q", c.call, r.st, c.running, c.lastExit)
			}
			if got := pre.afterGiveUp(); got != c.afterGiveUp {
				t.Fatalf("PreStart AfterGiveUp = %s, want %s", got, c.afterGiveUp)
			}
			if got := pre.replacesRestart(); got != c.replaces {
				t.Fatalf("PreStart ReplacesRestart = %s, want %s", got, c.replaces)
			}
			if got := pre.String(); got != c.automatic {
				t.Fatalf("PreStart Automatic = %s, want %s: no automatic start may follow the exit", got, c.automatic)
			}
			if c.onExit {
				// The run failed at once and restart: never does not restart it.
				if e := waitExit(t, exits, 10*time.Second, "a status waiting at the exit suppressed OnExit"); e != (Exit{StartFailed: true}) {
					t.Fatalf("OnExit got %+v, want a failed start", e)
				}
				return
			}
			// OnExit runs in a goroutine of its own, so its absence gets a wait.
			select {
			case e := <-exits:
				t.Fatalf("OnExit fired (%+v) for an exit a %s was waiting on", e, c.call)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

// k188: a run that ends on its own while the launcher shuts down is not
// reported to OnRunEnd. The restart the policy arms for it never comes —
// start refuses to start anything once ctx is done (k182) — so the console
// must not be told one is coming. The log keeps its lines, as before.
func TestNoRunEndDuringTheShutdown(t *testing.T) {
	var log lineLog
	sup := New(log.log)
	spec, _ := runsSpec(t, "f", 0) // exits 1 at once
	spec.RestartDelay = time.Hour
	var runEnds atomic.Int32
	spec.OnRunEnd = func(RunEnd) { runEnds.Add(1) }
	if err := sup.AddService(spec); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	taken := make(chan struct{})
	var once sync.Once
	sup.exitTaken = func() {
		once.Do(func() {
			cancel() // the shutdown comes while the loop acts on the exit
			close(taken)
		})
	}
	var wg sync.WaitGroup
	sup.Start(ctx, &wg)
	t.Cleanup(func() { cancel(); wg.Wait() })
	select {
	case <-taken:
	case <-time.After(10 * time.Second):
		t.Fatal("the exit was never taken")
	}
	wg.Wait()
	if !strings.Contains(log.String(), "[svc] restart 1 in 1h0m0s") {
		t.Fatalf("the premise is a restart armed and logged during the shutdown; log:\n%s", log.String())
	}
	if n := runEnds.Load(); n != 0 {
		t.Fatalf("OnRunEnd fired %d times during the shutdown, for a restart that never comes", n)
	}
}
