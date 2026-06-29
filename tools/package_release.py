#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import platform
import shutil
import stat
import subprocess
import zipfile
from pathlib import Path
from xml.sax.saxutils import escape


ROOT = Path(__file__).resolve().parents[1]
EXTENSION = ROOT / "extension"


def main() -> int:
    parser = argparse.ArgumentParser(description="Create a local LDGCC release bundle.")
    parser.add_argument("--out", default="dist/release", help="Release output directory.")
    args = parser.parse_args()

    release = (ROOT / args.out).resolve()
    work = release / "_work"
    if release.exists():
        shutil.rmtree(release)
    work.mkdir(parents=True)

    extension_stage = work / "extension"
    worker_stage = work / "worker-app"
    run(["python3", "tools/stage_extension.py", "--out", str(extension_stage)], ROOT)
    run(["python3", "tools/stage_worker_app.py", "--out", str(worker_stage)], ROOT)

    package = read_json(EXTENSION / "package.json")
    version = package["version"]
    platform_name = platform_key()

    vsix = release / "ldgcc-studio.vsix"
    worker_zip = release / f"ldgcc-worker-app-{platform_name}.zip"
    create_vsix(extension_stage, package, vsix)
    create_zip(worker_stage, worker_zip, "ldgcc-worker-app")

    install = release / "INSTALL.md"
    install.write_text(install_text(vsix.name, worker_zip.name), encoding="utf-8")

    artifacts = [vsix, worker_zip, install]
    checksums = release / "checksums.txt"
    write_checksums(checksums, artifacts)
    artifacts.append(checksums)

    manifest = release / "manifest.json"
    manifest.write_text(
        json.dumps(
            {
                "name": "ldgcc-v1-local-release",
                "version": version,
                "platform": platform_name,
                "artifacts": [{"path": artifact.name, "sha256": sha256(artifact)} for artifact in artifacts],
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )

    shutil.rmtree(work)
    print(f"release bundle: {release}", flush=True)
    return 0


def create_vsix(extension_stage: Path, package: dict, output: Path) -> None:
    files = sorted(path for path in extension_stage.rglob("*") if path.is_file())
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        write_zip_text(archive, "[Content_Types].xml", content_types_xml(extension_stage, files))
        write_zip_text(archive, "extension.vsixmanifest", vsix_manifest_xml(package))
        for file in files:
            archive_path = Path("extension") / file.relative_to(extension_stage)
            write_zip_file(archive, file, archive_path.as_posix())


def create_zip(source: Path, output: Path, root_name: str) -> None:
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for file in sorted(path for path in source.rglob("*") if path.is_file()):
            archive_path = Path(root_name) / file.relative_to(source)
            write_zip_file(archive, file, archive_path.as_posix())


def write_zip_file(archive: zipfile.ZipFile, source: Path, archive_path: str) -> None:
    info = zipfile.ZipInfo.from_file(source, archive_path)
    mode = source.stat().st_mode
    if mode & stat.S_IXUSR:
        info.external_attr = (0o755 & 0xFFFF) << 16
    archive.writestr(info, source.read_bytes())


def write_zip_text(archive: zipfile.ZipFile, archive_path: str, text: str) -> None:
    info = zipfile.ZipInfo(archive_path)
    info.external_attr = (0o644 & 0xFFFF) << 16
    archive.writestr(info, text.encode("utf-8"))


def content_types_xml(extension_stage: Path, files: list[Path]) -> str:
    overrides = []
    for file in files:
        if file.suffix == "":
            part_name = "/extension/" + file.relative_to(extension_stage).as_posix()
            overrides.append(f'  <Override PartName="{escape(part_name)}" ContentType="application/octet-stream" />')
    override_text = "\n".join(overrides)
    if override_text:
        override_text = "\n" + override_text
    return f"""<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="json" ContentType="application/json" />
  <Default Extension="js" ContentType="application/javascript" />
  <Default Extension="map" ContentType="application/json" />
  <Default Extension="md" ContentType="text/markdown" />
  <Default Extension="svg" ContentType="image/svg+xml" />
  <Default Extension="txt" ContentType="text/plain" />
  <Default Extension="xml" ContentType="text/xml" />
  <Default Extension="vsixmanifest" ContentType="text/xml" />
  <Default Extension="wasm" ContentType="application/wasm" />
  <Default Extension="lock" ContentType="application/json" />{override_text}
</Types>
"""


def vsix_manifest_xml(package: dict) -> str:
    extension_id = package["name"]
    publisher = package["publisher"]
    version = package["version"]
    display_name = package.get("displayName", extension_id)
    description = package.get("description", display_name)
    return f"""<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011">
  <Metadata>
    <Identity Language="en-US" Id="{escape(extension_id)}" Version="{escape(version)}" Publisher="{escape(publisher)}" />
    <DisplayName>{escape(display_name)}</DisplayName>
    <Description xml:space="preserve">{escape(description)}</Description>
    <Tags>machine-learning,distributed-training,ldgcc</Tags>
    <Categories>Other</Categories>
    <GalleryFlags>Public</GalleryFlags>
    <Properties>
      <Property Id="Microsoft.VisualStudio.Code.Engine" Value="{escape(package["engines"]["vscode"])}" />
      <Property Id="Microsoft.VisualStudio.Code.ExtensionKind" Value="workspace" />
    </Properties>
  </Metadata>
  <Installation>
    <InstallationTarget Id="Microsoft.VisualStudio.Code" />
  </Installation>
  <Dependencies />
  <Assets>
    <Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true" />
    <Asset Type="Microsoft.VisualStudio.Services.Content.Details" Path="extension/README.md" Addressable="true" />
    <Asset Type="Microsoft.VisualStudio.Services.Icons.Default" Path="extension/resources/ldgcc.svg" Addressable="true" />
  </Assets>
</PackageManifest>
"""


def install_text(vsix_name: str, worker_zip_name: str) -> str:
    return f"""# LDGCC V1 Local Release Install

This release contains the Brain Laptop package and Worker Laptop package.

## Brain Laptop

Install the VS Code extension:

```text
VS Code
    -> Extensions
    -> Install from VSIX
    -> select {vsix_name}
```

Then open a training project folder and use the LDGCC view:

```text
Start Master
    -> Discover Workers
    -> Pair Workers
    -> Prepare Job
    -> Set Up Workers
    -> Start Training
    -> Open Results
```

## Worker Laptop

Extract:

```text
{worker_zip_name}
```

Run:

```bash
cd ldgcc-worker-app
./run-worker-app.sh
```

Open the printed local URL, usually:

```text
http://127.0.0.1:5050
```

Then:

```text
Click Start Worker
    -> accept the pairing request
    -> leave Worker App running during setup/training
```

## Notes

This is a local Linux x64 release bundle when built on Linux x64. Build on each
target platform to produce platform-native binaries.
"""


def write_checksums(path: Path, artifacts: list[Path]) -> None:
    lines = [f"{sha256(artifact)}  {artifact.name}" for artifact in artifacts]
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def run(command: list[str], cwd: Path) -> None:
    print("+ " + " ".join(command), flush=True)
    subprocess.run(command, cwd=cwd, check=True)


def platform_key() -> str:
    system = platform.system().lower()
    if system == "darwin":
        node_platform = "darwin"
    elif system.startswith("win"):
        node_platform = "win32"
    else:
        node_platform = "linux"
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        node_arch = "x64"
    elif machine in {"aarch64", "arm64"}:
        node_arch = "arm64"
    else:
        node_arch = machine
    return f"{node_platform}-{node_arch}"


if __name__ == "__main__":
    raise SystemExit(main())
