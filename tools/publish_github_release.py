#!/usr/bin/env python3
import argparse
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def main() -> int:
    parser = argparse.ArgumentParser(description="Build and publish LDGCC artifacts to a GitHub Release.")
    parser.add_argument("tag", help="Release tag, for example v0.1.0.")
    parser.add_argument("--title", default="", help="Release title. Defaults to the tag.")
    parser.add_argument("--notes", default="", help="Release notes text.")
    parser.add_argument("--draft", action="store_true", help="Create the release as a draft.")
    parser.add_argument("--prerelease", action="store_true", help="Mark the release as a prerelease.")
    parser.add_argument("--skip-build", action="store_true", help="Upload existing dist/release artifacts.")
    args = parser.parse_args()

    release_dir = ROOT / "dist" / "release"
    if not args.skip_build:
        run(["python3", "tools/package_release.py"], ROOT)

    artifacts = [
        release_dir / "ldgcc-studio.vsix",
        release_dir / "ldgcc-worker-app-linux-x64.zip",
        release_dir / "ldgcc-worker-app-windows-x64.zip",
        release_dir / "INSTALL.md",
        release_dir / "manifest.json",
        release_dir / "checksums.txt",
    ]
    missing = [artifact for artifact in artifacts if not artifact.exists()]
    if missing:
        raise SystemExit("missing release artifacts: " + ", ".join(str(path) for path in missing))

    if shutil.which("gh") is None:
        raise SystemExit("GitHub CLI `gh` is required. Install it and run `gh auth login` first.")

    command = [
        "gh",
        "release",
        "create",
        args.tag,
        *[str(artifact) for artifact in artifacts],
        "--title",
        args.title or args.tag,
        "--notes",
        args.notes or default_notes(),
    ]
    if args.draft:
        command.append("--draft")
    if args.prerelease:
        command.append("--prerelease")
    run(command, ROOT)
    return 0


def default_notes() -> str:
    return (
        "LDGCC V1 local release artifacts.\n\n"
        "Download `ldgcc-studio.vsix` on the Brain laptop.\n"
        "Download the matching Worker App zip on each Worker laptop.\n"
        "See `INSTALL.md` for install steps."
    )


def run(command: list[str], cwd: Path) -> None:
    print("+ " + " ".join(command), flush=True)
    subprocess.run(command, cwd=cwd, check=True)


if __name__ == "__main__":
    raise SystemExit(main())
