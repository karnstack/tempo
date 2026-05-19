# 0057 — GitHub issue + PR templates, CODEOWNERS

## Files changed

- `.github/ISSUE_TEMPLATE/config.yml` — blank issues disabled,
  Discussions link as the contact route.
- `.github/ISSUE_TEMPLATE/bug_report.yml` — structured Issue Form
  with summary, repro, expected/actual, version, connection kind,
  env, logs.
- `.github/ISSUE_TEMPLATE/feature_request.yml` — problem /
  proposal / alternatives / audience.
- `.github/PULL_REQUEST_TEMPLATE.md` — summary, why, how-to-test,
  screenshots, checklist.
- `.github/CODEOWNERS` — `* @karngyan`.

## Verify output

```
  .github/ISSUE_TEMPLATE/config.yml ok
  .github/ISSUE_TEMPLATE/bug_report.yml ok
  .github/ISSUE_TEMPLATE/feature_request.yml ok
  .github/PULL_REQUEST_TEMPLATE.md ok
  .github/CODEOWNERS ok
  .github/ISSUE_TEMPLATE/bug_report.yml parses
  .github/ISSUE_TEMPLATE/config.yml parses
  .github/ISSUE_TEMPLATE/feature_request.yml parses
VERIFY OK
```

## Notes / followups

- **YAML validation via a one-shot Go program** using
  `gopkg.in/yaml.v3`. The original verify.sh assumed `python3 -c
  "import yaml"` worked; PyYAML wasn't installed in the dev env
  here. The Go fallback works in any tempo dev environment because
  the module already depends on `gopkg.in/yaml.v3` transitively via
  kin-openapi (0045).
- **Discussions URL is a placeholder** — points at
  https://github.com/karnstack/tempo/discussions; harmless if
  Discussions isn't enabled yet (the link 404s gracefully).
- **No SECURITY.md / dependabot.yml.** Out of scope per the TASK
  body; the latter belongs to the CI task (0058).
