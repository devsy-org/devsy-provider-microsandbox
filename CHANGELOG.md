# Changelog

## [1.0.6](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.5...v1.0.6) (2026-11-08)


### Bug Fixes

* **deps:** update module github.com/superradcompany/microsandbox/sdk/go to v0.7.14 ([#71](https://github.com/devsy-org/devsy-provider-microsandbox/issues/71)) ([13ca184](https://github.com/devsy-org/devsy-provider-microsandbox/commit/13ca1846627879e7bf9036495abaf459b8236f2d))

## [1.0.5](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.4...v1.0.5) (2026-11-08)


### Bug Fixes

* **msb:** keep devcontainer metadata off the msb command line ([#67](https://github.com/devsy-org/devsy-provider-microsandbox/issues/67)) ([670d2df](https://github.com/devsy-org/devsy-provider-microsandbox/commit/670d2df15783ab98c2e3aa9fa5cf010df71ac300))
* **msb:** report the real msb error and resolve symlinked mount paths ([#68](https://github.com/devsy-org/devsy-provider-microsandbox/issues/68)) ([4ec8455](https://github.com/devsy-org/devsy-provider-microsandbox/commit/4ec8455cbd364a0687b6570e1fc893e84921d210))
* **msb:** surface configuration errors to the user ([#70](https://github.com/devsy-org/devsy-provider-microsandbox/issues/70)) ([210b280](https://github.com/devsy-org/devsy-provider-microsandbox/commit/210b28020aa4dd6a9fc425eb3983f2b4a9127be8))

## [1.0.4](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.3...v1.0.4) (2026-11-07)


### Bug Fixes

* **msb:** pass guest environment without putting values on argv ([#64](https://github.com/devsy-org/devsy-provider-microsandbox/issues/64)) ([1b5b3ee](https://github.com/devsy-org/devsy-provider-microsandbox/commit/1b5b3ee271634691eefb948c02a499dce7157919))

## [1.0.3](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.2...v1.0.3) (2026-11-06)


### Bug Fixes

* **deps:** update module github.com/stretchr/testify to v1.12.1 ([#54](https://github.com/devsy-org/devsy-provider-microsandbox/issues/54)) ([acc1a0f](https://github.com/devsy-org/devsy-provider-microsandbox/commit/acc1a0f683b2b43180d580b23211062f826f509b))
* **deps:** update module google.golang.org/grpc to v1.85.0 ([#55](https://github.com/devsy-org/devsy-provider-microsandbox/issues/55)) ([1c22091](https://github.com/devsy-org/devsy-provider-microsandbox/commit/1c220913297c3f4e50edfa91846ff6350d7d8b26))
* **microsandbox:** release guest stdin deterministically after cancellation ([#52](https://github.com/devsy-org/devsy-provider-microsandbox/issues/52)) ([a8d1ee6](https://github.com/devsy-org/devsy-provider-microsandbox/commit/a8d1ee63401e6c7290bcd1ab47eca169d876c933))

## [1.0.2](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.1...v1.0.2) (2026-11-05)


### Bug Fixes

* **deps:** update module github.com/google/go-containerregistry to v0.22.1 ([#49](https://github.com/devsy-org/devsy-provider-microsandbox/issues/49)) ([2050659](https://github.com/devsy-org/devsy-provider-microsandbox/commit/20506590b5f2042d65abbe6f5572c4cb072ef26d))
* **microsandbox:** report actionable version and inspect errors ([#46](https://github.com/devsy-org/devsy-provider-microsandbox/issues/46)) ([f950f2b](https://github.com/devsy-org/devsy-provider-microsandbox/commit/f950f2bf0a0b3d41ffda165048f04f646f4eb806))

## [1.0.1](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v1.0.0...v1.0.1) (2026-11-05)


### Bug Fixes

* **lint:** restore doc comments and exported rule checks ([#42](https://github.com/devsy-org/devsy-provider-microsandbox/issues/42)) ([2387018](https://github.com/devsy-org/devsy-provider-microsandbox/commit/238701862448fec1dfeafeed276a166b0567ddcb))

## [1.0.0](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.12...v1.0.0) (2026-11-05)


### ⚠ BREAKING CHANGES

* **microsandbox:** support MicroSandbox 0.7.13 ([#37](https://github.com/devsy-org/devsy-provider-microsandbox/issues/37))

### Features

* **microsandbox:** support MicroSandbox 0.7.13 ([#37](https://github.com/devsy-org/devsy-provider-microsandbox/issues/37)) ([dda4168](https://github.com/devsy-org/devsy-provider-microsandbox/commit/dda4168a8cd40ce8d15ef5367e7d5ec017e1c857))

## [0.1.12](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.11...v0.1.12) (2026-11-05)


### Features

* **microsandbox:** bind explicit runtime pair and check compatibility ([#35](https://github.com/devsy-org/devsy-provider-microsandbox/issues/35)) ([3ca20a4](https://github.com/devsy-org/devsy-provider-microsandbox/commit/3ca20a4af71747631a1b15b18a7c537ba1c8c208))

## [0.1.11](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.10...v0.1.11) (2026-10-16)


### Bug Fixes

* align MicroSandbox provider description ([5f3429b](https://github.com/devsy-org/devsy-provider-microsandbox/commit/5f3429bcc93915ac4d894ff6d39f67a47f40aff3))

## [0.1.10](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.9...v0.1.10) (2026-10-16)


### Bug Fixes

* support Devsy managed secret tmpfs mounts ([316ded3](https://github.com/devsy-org/devsy-provider-microsandbox/commit/316ded37b34f69d8ae858ea46dfb570d71e2d999))

## [0.1.9](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.8...v0.1.9) (2026-10-16)


### Bug Fixes

* accept Docker mount consistency hints ([53d148f](https://github.com/devsy-org/devsy-provider-microsandbox/commit/53d148f51065de34cf638ba92ddbb1e0e23bfbdd))

## [0.1.8](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.7...v0.1.8) (2026-10-13)


### Features

* **runtime:** recreate MicroSandbox on authorized RunImage ([9a7f1b4](https://github.com/devsy-org/devsy-provider-microsandbox/commit/9a7f1b4da922f3d64b8a91d6b4cff2fc474b3975))

## [0.1.7](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.6...v0.1.7) (2026-10-11)


### Bug Fixes

* **runtime:** use runtime default for ephemeral disk sizing ([d9691ce](https://github.com/devsy-org/devsy-provider-microsandbox/commit/d9691ce28a63a550a851eb014126f64ac36cb586))

## [0.1.6](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.5...v0.1.6) (2026-10-10)


### Bug Fixes

* **distribution:** preserve off policy in provider manifest ([442d5eb](https://github.com/devsy-org/devsy-provider-microsandbox/commit/442d5eb7f4612e5766984e04560e080b8cd87ca3))

## [0.1.5](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.4...v0.1.5) (2026-10-09)


### Bug Fixes

* **runtime:** report stopped VM state in Find ([99bc8cc](https://github.com/devsy-org/devsy-provider-microsandbox/commit/99bc8cc8eca754b48f94bcffacccd2d5bb3c0c01))

## [0.1.4](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.3...v0.1.4) (2026-10-08)


### Features

* **runtime:** validate MicroSandbox workspace reuse ([dedd759](https://github.com/devsy-org/devsy-provider-microsandbox/commit/dedd75962c37c8e6af056a86f8fd2959ee40f8d3))

## [0.1.3](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.2...v0.1.3) (2026-10-08)


### Features

* **microsandbox:** package checksum-pinned external provider releases ([d771c4f](https://github.com/devsy-org/devsy-provider-microsandbox/commit/d771c4f3b0cf2c347acd28a0ba9eaca89a8fe378))
* **microsandbox:** serve Runtime v1 command and log streams ([#9](https://github.com/devsy-org/devsy-provider-microsandbox/issues/9)) ([670a1e4](https://github.com/devsy-org/devsy-provider-microsandbox/commit/670a1e484338ea4133fa5b1dd15b582672bc19f3))

## [0.1.2](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.1...v0.1.2) (2026-10-07)


### Features

* **microsandbox:** add unary Runtime v1 server foundation ([#8](https://github.com/devsy-org/devsy-provider-microsandbox/issues/8)) ([d0c652a](https://github.com/devsy-org/devsy-provider-microsandbox/commit/d0c652ae9d506cf741c30576d738500055d1f463))
* **msb:** resolve workspace owners from image snapshots ([#6](https://github.com/devsy-org/devsy-provider-microsandbox/issues/6)) ([06bcacb](https://github.com/devsy-org/devsy-provider-microsandbox/commit/06bcacb090aa8ae4e49be33ab47384e3ce84b065))

## [0.1.1](https://github.com/devsy-org/devsy-provider-microsandbox/compare/v0.1.0...v0.1.1) (2026-10-07)


### Features

* **msb:** import validated image snapshots ([#4](https://github.com/devsy-org/devsy-provider-microsandbox/issues/4)) ([13f98f2](https://github.com/devsy-org/devsy-provider-microsandbox/commit/13f98f20064dbd4624922a66ce36034f4325843e))

## 0.1.0 (2026-10-07)


### Features

* extract MicroSandbox runtime client ([#2](https://github.com/devsy-org/devsy-provider-microsandbox/issues/2)) ([237c489](https://github.com/devsy-org/devsy-provider-microsandbox/commit/237c489d92ccc0884cec4f4cd107df096f9fb556))
