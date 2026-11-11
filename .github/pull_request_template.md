<!--
Thanks for the pull request! Please read CONTRIBUTING.md first:
https://github.com/visveshkhanna/aka/blob/main/CONTRIBUTING.md
-->

## What this changes

<!-- What does this change, and why? Link the issue it fixes, e.g. "Fixes #12". -->

## How it was tested

<!-- Tests added, and anything you ran by hand: the OS, the command, the dev server. -->

## Checklist

- [ ] `gofmt -l .` prints nothing, and `go vet ./...` passes (also with `GOOS=windows` and `GOOS=linux` for platform code)
- [ ] `go test -race ./...` passes, with tests for the new behaviour or the fixed bug
- [ ] User-facing changes have a `CHANGELOG.md` entry under `Unreleased` and updated docs in `website/content/docs`
- [ ] Each commit is one logical change with a clear message
