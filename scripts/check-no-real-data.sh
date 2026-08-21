#!/usr/bin/env bash
# 공개 레포에 실계정·실번호·실키가 섞이는 것을 막는다.
#
# 예시는 전부 명백한 더미로 고정한다:
#   번호   01000000000 / 07000000000
#   계정   AC00000000000000000000000000000000
#   통화   CA00000000000000000000000000000000
#
# go.sum 등 의존성 잠금 파일은 base64 해시가 숫자열과 충돌해 제외한다.
set -euo pipefail

EXCLUDES=(
  --exclude-dir=.git --exclude-dir=dist --exclude-dir=vendor
  --exclude=go.sum --exclude=go.mod
  --exclude=check-no-real-data.sh
)

fail=0
scan() {
  local pattern="$1" label="$2" hits
  # 0 만으로 이뤄진 더미(00000000...)는 통과시킨다.
  hits=$(grep -rInE "$pattern" "${EXCLUDES[@]}" . | grep -vE '0{8,}' || true)
  if [ -n "$hits" ]; then
    echo "실데이터 의심 ($label) — 더미 값으로 바꾸세요"
    echo "$hits"
    fail=1
  fi
}

scan '(^|[^0-9])01[016789][0-9]{7,8}([^0-9]|$)' "휴대전화번호"
scan '(^|[^0-9])070[0-9]{7,8}([^0-9]|$)'        "070 번호"
scan '\bAC[0-9a-f]{32}\b'                       "계정 ID"
scan '\bCA[0-9a-f]{32}\b'                       "통화 ID"
scan '\bsk_(live|test)_[A-Za-z0-9]{8,}'         "API 키"

if [ "$fail" -eq 0 ]; then echo "실데이터 스캔 통과"; fi
exit $fail
