# Repository Guidelines

## Project Structure & Module Organization
This repository combines Azure infrastructure, GitOps manifests, and a Backstage portal.

- `terraform/`: provisions the AKS control plane and bootstrap resources.
- `gitops/`: Argo CD applications, cluster definitions, environment overlays, and hooks.
- `backstage/`: Yarn workspace for the portal. Main packages live in `packages/app` and `packages/backend`; custom plugins live in `plugins/`.
- `docs/` and `_docs/`: guidance and working drafts.
- `images/`: diagrams referenced by documentation.

## Build, Test, and Development Commands
Run infrastructure commands from `terraform/` and portal commands from `backstage/`.

- `cd terraform && terraform init -upgrade`: initialize providers.
- `cd terraform && terraform apply ...`: provision or update the platform environment.
- `cd terraform && terraform fmt -recursive && terraform validate`: normalize and validate Terraform before review.
- `cd backstage && yarn install`: install Backstage workspace dependencies.
- `cd backstage && yarn dev`: run frontend and backend locally.
- `cd backstage && yarn build:all`: build Backstage packages.
- `cd backstage && yarn test` or `yarn test:all`: run unit tests; `test:all` adds coverage.
- `cd backstage && yarn test:e2e`: run Playwright end-to-end tests.
- `cd backstage && yarn lint:all && yarn prettier:check`: run lint and format checks.

## Coding Style & Naming Conventions
Use the existing toolchain instead of ad hoc formatting.

- TypeScript follows Backstage ESLint defaults and Prettier via `@spotify/prettier-config`.
- Prefer PascalCase for React components, camelCase for functions/variables, and `*.test.ts(x)` / `*.spec.ts` for tests.
- Keep Kubernetes and Argo CD manifests descriptive and directory-scoped, for example `gitops/environments/default/addons/...`.
- Run `terraform fmt -recursive` for `.tf` files and keep YAML indentation consistent at two spaces.

## Testing Guidelines
Backstage unit tests live beside source files, such as `packages/app/src/App.test.tsx`. Playwright tests live in `packages/app/e2e-tests/`.

- Add or update tests for portal behavior changes.
- Run `yarn test:all` for code changes and `yarn test:e2e` when UI flows or auth behavior change.
- For infrastructure changes, at minimum run `terraform validate` and review rendered GitOps manifests affected by the change.

## Commit & Pull Request Guidelines
Recent history mixes short imperative commits (`finalize PRD`) with conventional maintenance commits (`dependabot(deps): ...`) and issue-linked fixes (`Fix ... (#129)`). Prefer concise, imperative subjects and include the issue or PR reference when available.

PRs should explain scope, call out affected areas (`terraform/`, `gitops/`, `backstage/`), and include screenshots for Backstage UI changes. Note required operator steps, secret handling, or Azure-side prerequisites when they affect rollout or testing.
