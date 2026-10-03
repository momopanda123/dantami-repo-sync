# Changelog / 변경 이력

Published version tags and installer assets are retained. New versions are added as separate releases; existing releases are not overwritten.
배포된 버전의 태그와 설치파일을 보존하며 새 버전은 별도 릴리즈로 추가합니다.

## v1.6.2

- After service restart, enabled repository pairs check first and immediately synchronize safe pending changes
- Resume the configured interval after synchronization; explicit checks remain read-only
- Preserve pause, conflict, failed-check and state-persistence safeguards
- 서비스 재시작 후 자동 실행 중인 연결은 첫 비교 직후 안전한 변경을 바로 반영하고 이후 설정 주기로 실행

## v1.6.1

- Match Settings navigation to the existing sidebar styles; remove its special border and color
- Show only the selected language in the Settings label
- 설정 메뉴의 별도 테두리·강조색과 한영 혼합 표기를 제거하고 기존 메뉴와 통일

## v1.6.0

- Add a discoverable Settings menu with Korean/English language selection and installed version
- Persist browser-only dashboard refresh and new-connection defaults; add a manual Refresh button
- Let new connections override their initial interval without modifying existing pairs or enabling sync
- Keep login language switching and add a one-year language preference cookie
- Improve compact navigation and verify cancellation, storage failures, read-only-user access, and bilingual UI
- 설정 메뉴·언어 전환·화면 갱신 간격·새 연결 기본값 추가. 기존 연결과 자동 실행 상태는 변경하지 않음

## Documentation updates after v1.5.0

- Illustrated English/Korean quickstart using anonymized real-use screenshots
- Step-by-step token setup, repository pairing, first sync and troubleshooting
- No change to the published v1.5.0 installer or tag

## [v1.5.0](https://github.com/momopanda123/dantami-repo-sync/releases/tag/v1.5.0)

- Korean/English app language selection and saved browser preference
- Translated status, errors, dialogs and account management
- Bilingual documentation and installer instructions
- MIT license

## [v1.4.1](https://github.com/momopanda123/dantami-repo-sync/releases/tag/v1.4.1)

- Current-token Git access checks for repository candidates
- Revalidation before registering a pair and rejection of stale lists
- Independent app accounts, repository management and synchronization
- Historical installer preserved unchanged; release listing added retrospectively
