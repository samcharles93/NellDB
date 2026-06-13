# Changelog

All notable changes to this project will be documented in this file.

## [v0.1.10] - 2026-06-13

### Changed
- Minimalist README rewrite focused on library usage and structure.
- Removed AI-generated "slop" and generic marketing language.

## [v0.1.9] - 2026-06-13

### Fixed
- Import syntax error in `logstore/log.go` causing build failures.
- Durable storage replay logic: moved discard check before frame append to correctly handle truncated files and avoid EOF errors on startup.

## [v0.1.8] - 2026-06-13

### Changed
- Initial feature-complete release of core engine, logstore, SDK, and HTTP sync.
