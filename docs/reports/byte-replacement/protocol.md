# Byte replacement experiment

Status: research in progress, based on Semantix main `f527fd7`.

The existing agent appends L2 content before the latest user message; it does
not remove original context. `kernel/inject.Injector.BuildHits` supplies the
actual L2 block for comparison. An explicitly managed context block can be
replaced in the provider request copy without changing the saved transcript.

Three comparisons on the same GLM-5.3-Flash task and endpoint:

- A: original project context.
- B: original context plus the actual Semantix L2 block.
- D: replace it with the same facts and field names in compact JSON.

Replacement requires exactly one bounded user-role context block, the expected
project and revision, all known fields and string values, and no duplicate or
unknown fields. Invalid or ambiguous blocks are left unchanged. It never edits
system instructions or the current user question. This is a small structured
context experiment; it does not prove arbitrary prose equivalence.

The report records each request, answer and provider-reported usage. Compare
actual prompt tokens and answer facts first; cache discounts and total estimated
cost need current official tariff confirmation. A few synthetic tasks can reveal
whether this route is promising but cannot establish production-wide quality or
savings. Only expand to real Semantix workloads if the direct comparison works.

The earlier GLM-4.7 preflight established connectivity only; its model differs
from the requested GLM-5.3-Flash. The GLM-5.3-Flash attempt on the ordinary
API returned HTTP 429, code 1113. The user's remaining quota is for GLM Coding
Plan, whose endpoint and supported-tool scope are separate. Its comparison is
pending ordinary API resources. The credential was read via a no-echo prompt
and was not saved.

Official endpoint: https://docs.bigmodel.cn/cn/guide/develop/http/introduction
Coding Plan endpoint and scope: https://docs.bigmodel.cn/cn/coding-plan/quick-start
