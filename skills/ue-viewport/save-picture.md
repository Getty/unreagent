# UE viewport capture: the picture as a file

Through the bridge a capture is only useful for its other fields: the base64
string costs as much context as the whole JPEG, and unreagent's `read_file`
returns text only (skill `unreagent-files`). To look at the picture, get it
into a file and open that file with a reader of your client that shows
images.

In PIE, `gameplay_debug` `run_sequence` writes JPEG files itself: its
`capture` steps and auto-capture (skill `ue-pie-testing`, section
`sequences`). For every other case, use the sketch below.

## Through run_python

The plugin also answers plain HTTP. A script on the host can call the tool
there, decode the image and print only the file's path. `run_python` is off
unless the project enables it (skill `unreagent-runtimes`). This is a sketch,
not exercised against an editor:

```python
import base64, json, os, urllib.error, urllib.request

# The bridge's UNREAL_MCP_URL; the port is 3000 unless the editor was
# started with -MCPPort=<n>.
URL = "http://127.0.0.1:3000/mcp/tool/capture_viewport"
params = {"asset_path": "/Game/Characters/SK_Hero.SK_Hero", "camera": "front"}
out = os.path.abspath("Saved/agent-captures/capture.jpg")

req = urllib.request.Request(URL, data=json.dumps(params).encode(),
                             headers={"Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=60) as r:
        res = json.load(r)
except urllib.error.HTTPError as e:  # a failed tool answers with HTTP 400
    res = json.load(e)
if not res.get("success"):
    # Tool failures carry "message"; the server's own errors carry "error".
    # A string passed to SystemExit is printed and gives exit code 1.
    raise SystemExit("capture failed: " + str(
        res.get("message") or res.get("error") or json.dumps(res)))

os.makedirs(os.path.dirname(out), exist_ok=True)
with open(out, "wb") as f:
    f.write(base64.b64decode(res["data"]["image_base64"]))
data = {k: v for k, v in res["data"].items() if k != "image_base64"}
print(out)
print(json.dumps(data))  # the camera and size fields, without the image
```

- The body is the tool's arguments as they are; the answer is
  `{success, message, data}`, without a task around it. Errors of the HTTP
  server itself (a bad body, no tool name) come as `{success: false, error}`.
- Unknown parameter names are reported here: the message gains
  `WARNING: Unknown parameter ignored: <name>. Valid params: …` (`parameters`
  for several). The call still runs without them.
- The relative path lands in the runtime's working directory, normally the
  project. Use a new file name per capture when you compare pictures, such as
  `before.jpg` and `after.jpg`.
- Change `params` for the other modes: `{}` for the level or PIE view,
  `{"camera_location": {...}, "camera_rotation": {...}}` for your own camera.

## Only while the editor is idle

Call directly only while no other toolkit call, script, import or
`run_sequence` is in progress. Which thread handles the request is up to the
engine's HTTP server; the plugin's comments say one other than the game
thread. Then a direct call waits at most 30 s for the game thread to run it,
and answers `Tool 'capture_viewport' execution timed out after 30 seconds`.

That answer is not an ordinary error. From the plugin's code
(`UnrealClaudeMCPServer.cpp`, an upstream bug), the job is still queued on the
game thread when the answer goes out, and when it runs later it writes into
memory the plugin has already given back. That can crash the editor. **Do not
retry**: check with unreagent's `status` that the editor still runs (skill
`unreagent-iterate`) before anything else.

On the game thread there is no such answer: the sketch's own 60 s timeout ends
the wait with a Python exception (`TimeoutError: timed out`), and the capture
may still run. Skill `ue-tasks`, "A 30 s timeout can crash the editor", covers
both cases for every toolkit call.
