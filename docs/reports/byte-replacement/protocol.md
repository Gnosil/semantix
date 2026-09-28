# Byte replacement experiment

Status: research in progress, based on Semantix main `f527fd7`.

The existing agent appends L2 content before the latest user message; it does
not remove original context. `kernel/inject.Injector.BuildHits` supplies the
actual L2 block for comparison. An explicitly managed context block can be
replaced in the provider request copy without changing the saved transcript.

Three arms for each GLM-5.3-Flash task on the Coding Plan endpoint:

- A: original project context.
- B: original context plus the actual Semantix L2 block.
- D: replace it with the same facts and field names in compact JSON.

Replacement requires exactly one bounded user-role context block, the expected
project and revision, all known fields and string values, and no duplicate or
unknown fields. Invalid or ambiguous blocks are left unchanged. It never edits
system instructions or the current user question. This is a small structured
context experiment; it does not prove arbitrary prose equivalence.

The report records each request, answer and provider-reported usage. Compare
actual prompt tokens and answer facts first; Coding Plan point or monetary
cost cannot be inferred from tokens alone. Three synthetic tasks show the
structured replacement route is promising but cannot establish production-wide
quality or savings. The next step is real Semantix workloads.

The opt-in runtime mode `semantix.mode = "replace"` handles only a single
admitted Context slice whose content exactly matches one earlier, uniquely
bounded `<semantix-managed-context project="..." revision="...">` user
message. Its project and revision must match the slice provenance. It removes
insignificant whitespace from the JSON object and omits the duplicate L2
block in the provider request copy; the saved message is untouched. All
other cases retain the existing injection behavior.

The earlier GLM-4.7 preflight established connectivity only; its model differs
from the requested GLM-5.3-Flash. The GLM-5.3-Flash attempt on the ordinary
API returned HTTP 429, code 1113. The user's remaining quota is for GLM Coding
Plan, whose endpoint and supported-tool scope are separate. The same model
completed all nine requests on the Coding Plan endpoint. The credential was
read via a no-echo prompt and was not saved.

Official endpoint: https://docs.bigmodel.cn/cn/guide/develop/http/introduction
Coding Plan endpoint and scope: https://docs.bigmodel.cn/cn/coding-plan/quick-start
