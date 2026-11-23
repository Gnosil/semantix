# DeepSeek Harness plugin verification

Windows amd64, DSH `0.1.5-rc.1`, Node `24.15.0`, and Semantix built from source.
The test installed the local bundle into an isolated `headless` profile and
used a loopback Chat Completions stub. It checked the following behavior:

1. A fresh store provided no recall. The first completed turn saved both the
   prompt and assistant reply, and `semantix search` found the saved reply.
2. A fresh DSH process in the same project received recalled memory in its
   first outbound model request.
3. The same prompt in another project did not receive that memory.
4. With the Semantix executable removed from PATH, DSH completed the turn.
5. After removing the bundle, automatic recall stopped and the store remained.

Run `plugins/semantix-dsh-memory/smoke.py` to repeat the test. The bundled
manifest, entry point, localization files, and icon were checked with
`npm pack --dry-run`. The DSH graphical interface was outside this test.
