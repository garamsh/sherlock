#!/usr/bin/env bash
# The supported snapshot and restore of one agent's stored state
# (docs/architecture/deployment.md §Snapshot and restore). Three verbs:
#
#   snapshot.sh copy    <memory-path> <dest>   copy a stopped writer's store
#   snapshot.sh verify  <dest>                 check the copy and report it
#   snapshot.sh restore <dest> <memory-path>   lay the copy at a fresh path
#
# The stored state is the memory file, its optional -wal and -shm, and the
# outbox/ directory beside it. A file that is absent is recorded as absent
# and stays absent through restore: a cleanly closed store has neither
# sidecar, and inventing one is not a copy.
set -euo pipefail

die() {
  echo "snapshot: $*" >&2
  exit 1
}

usage() {
  sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

sha() { sha256sum "$1" | cut -d' ' -f1; }

# manifest_of <root> <base> <outbox-dir> [<label>] prints the manifest of
# the state under root: one `file <sha256> <name>` or `absent <name>` line
# per store file, then `outbox present|absent`, then one `file` line per
# outbox file. Store files are read as <base>* and named as <label>*, which
# defaults to base, so a restore under another name compares equal. Outbox
# paths are sorted, so two manifests of the same state are equal.
manifest_of() {
  local root=$1 base=$2 outbox=$3 label=${4:-$2} suffix rel
  echo "sherlock-snapshot 1"
  echo "store $label"
  for suffix in "" -wal -shm; do
    if [ -e "$root/$base$suffix" ]; then
      [ -f "$root/$base$suffix" ] || die "$root/$base$suffix is not a regular file"
      echo "file $(sha "$root/$base$suffix") $label$suffix"
    else
      echo "absent $label$suffix"
    fi
  done
  if [ -d "$outbox" ]; then
    echo "outbox present"
    while read -r rel; do
      echo "file $(sha "$outbox/$rel") outbox/$rel"
    done < <(cd "$outbox" && find . -type f -printf '%P\n' | LC_ALL=C sort)
  else
    echo "outbox absent"
  fi
}

# with_free_lock <real-memory-path> <command...> runs the command holding
# the store's writer lock, and refuses when another process holds it. The
# lock is the one the agent takes (<real path>.lock), so holding it for the
# whole copy also keeps a writer on this host from starting mid-copy.
with_free_lock() {
  local lock="$1.lock"
  shift
  exec {fd}<>"$lock" || die "cannot open the writer lock $lock"
  if ! flock -n -x "$fd"; then
    die "$lock is held: the writer has not stopped, so no consistent copy can be taken and the copy must not be activated"
  fi
  "$@"
  exec {fd}>&-
}

copy_store() {
  local src_dir=$1 base=$2 dest=$3 suffix
  mkdir "$dest"
  for suffix in "" -wal -shm; do
    if [ -e "$src_dir/$base$suffix" ]; then
      cp --preserve=mode,timestamps "$src_dir/$base$suffix" "$dest/$base$suffix"
    fi
  done
  if [ -d "$src_dir/outbox" ]; then
    cp -R --preserve=mode,timestamps "$src_dir/outbox" "$dest/outbox"
  fi
  manifest_of "$src_dir" "$base" "$src_dir/outbox" >"$dest/manifest"
  # The copy is read back against the source manifest, so a file that
  # changed under the copy, or did not copy whole, fails here.
  diff -u "$dest/manifest" <(manifest_of "$dest" "$base" "$dest/outbox") >&2 ||
    die "the copy at $dest does not match its source"
}

cmd_copy() {
  [ $# -eq 2 ] || usage
  local memory_path=$1 dest=$2 real
  [ ! -e "$dest" ] || die "$dest exists; a snapshot goes to a fresh path"
  real=$(realpath -e "$memory_path") || die "$memory_path does not exist"
  with_free_lock "$real" copy_store "$(dirname "$real")" "$(basename "$real")" "$dest"
  echo "snapshot: copied $real to $dest"
}

# check_hashes <dest> confirms dest still holds exactly what its manifest
# records, absent files included, and nothing beside it.
check_hashes() {
  local dest=$1 base
  [ -f "$dest/manifest" ] || die "$dest/manifest is missing"
  base=$(sed -n 's/^store //p' "$dest/manifest")
  [ -n "$base" ] || die "$dest/manifest names no store"
  diff -u "$dest/manifest" <(manifest_of "$dest" "$base" "$dest/outbox") >&2 ||
    die "$dest no longer matches its manifest"
  local entry
  for entry in "$dest"/* "$dest"/.[!.]*; do
    [ -e "$entry" ] || continue
    case "${entry##*/}" in
    manifest | outbox | "$base" | "$base-wal" | "$base-shm") ;;
    *) die "$dest holds $entry, which its manifest does not name" ;;
    esac
  done
  echo "$base"
}

cmd_verify() {
  [ $# -eq 1 ] || usage
  local dest=$1 base scratch suffix
  base=$(check_hashes "$dest")
  # The count is read from a scratch duplicate: opening a store can
  # checkpoint its WAL or create the sidecars, and the snapshot stays the
  # bytes it was copied as.
  scratch=$(mktemp -d)
  trap 'rm -rf "$scratch"' RETURN
  for suffix in "" -wal -shm; do
    [ ! -e "$dest/$base$suffix" ] || cp "$dest/$base$suffix" "$scratch/$base$suffix"
  done
  python3 - "$scratch/$base" "$dest/outbox" <<'PY'
import hashlib, json, os, sqlite3, sys

db, outbox = sys.argv[1], sys.argv[2]
digest = lambda text: hashlib.sha256(text.encode()).hexdigest()
con = sqlite3.connect(db)
q = lambda sql: con.execute(sql).fetchall()
integrity = q("PRAGMA integrity_check")[0][0]
note = q("SELECT content FROM conversation_note WHERE id = 1")
report = {
    "integrity": integrity,
    "sources": q("SELECT COUNT(*) FROM conversation_sources")[0][0],
    "completions": [
        {"sender": s, "id": i}
        for s, i in q("SELECT sender, external_id FROM conversation_sources "
                      "WHERE role = 'user' AND external_id IS NOT NULL AND external_id != '' "
                      "ORDER BY id")
    ],
    "episodes": [
        {"id": i, "source_first": f, "source_last": l, "summary_sha256": digest(s),
         "embedded": e is not None}
        for i, f, l, s, e in q("SELECT id, source_first, source_last, summary, embedding "
                                "FROM conversation_mid ORDER BY id")
    ],
    "note": {"present": bool(note and note[0][0]),
             "sha256": digest(note[0][0]) if note else None},
    "outbox": [],
}
if os.path.isdir(outbox):
    for name in sorted(os.listdir(outbox)):
        if not name.endswith(".json") or name.startswith("."):
            continue
        with open(os.path.join(outbox, name)) as f:
            entry = json.load(f)
        report["outbox"].append({
            "file": name,
            "id": entry.get("id"),
            "created_at": entry.get("created_at"),
            "agent_id": entry.get("agent_id"),
            "assignment_epoch": entry.get("assignment_epoch"),
            "generation": entry.get("generation"),
            "in_reply_to": entry.get("in_reply_to"),
            "content_sha256": digest(entry.get("content", "")),
        })
con.close()
json.dump(report, sys.stdout, sort_keys=True)
print()
if integrity != "ok":
    sys.exit("snapshot: integrity_check reported " + integrity)
PY
}

restore_store() {
  local dest=$1 target_dir=$2 target_base=$3 base=$4 suffix
  for suffix in "" -wal -shm; do
    if [ -e "$dest/$base$suffix" ]; then
      cp --preserve=mode,timestamps "$dest/$base$suffix" "$target_dir/$target_base$suffix"
    fi
  done
  if [ -d "$dest/outbox" ]; then
    cp -R --preserve=mode,timestamps "$dest/outbox" "$target_dir/outbox"
  fi
  diff -u "$dest/manifest" <(manifest_of "$target_dir" "$target_base" "$target_dir/outbox" "$base") >&2 ||
    die "the restored state at $target_dir does not match $dest"
}

cmd_restore() {
  [ $# -eq 2 ] || usage
  local dest=$1 memory_path=$2 base target_dir target_base suffix
  base=$(check_hashes "$dest")
  target_dir=$(dirname "$memory_path")
  target_base=$(basename "$memory_path")
  mkdir -p "$target_dir"
  target_dir=$(realpath -e "$target_dir")
  for suffix in "" -wal -shm; do
    [ ! -e "$target_dir/$target_base$suffix" ] || die "$target_dir/$target_base$suffix exists; a restore goes to a fresh path and replaces nothing"
  done
  [ ! -e "$target_dir/outbox" ] || die "$target_dir/outbox exists; a restore goes to a fresh path and replaces nothing"
  with_free_lock "$target_dir/$target_base" restore_store "$dest" "$target_dir" "$target_base" "$base"
  echo "snapshot: restored $dest to $target_dir/$target_base"
}

[ $# -ge 1 ] || usage
verb=$1
shift
case "$verb" in
copy) cmd_copy "$@" ;;
verify) cmd_verify "$@" ;;
restore) cmd_restore "$@" ;;
*) usage ;;
esac
