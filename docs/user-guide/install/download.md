# Download

Pick the computer that will run the tournament app: usually the laptop at the desk. Each section of this page gives the recommended route for it, and the command that starts the app. [Install](install.md) has every other way, and how to upgrade.

Once the app is running, open `http://localhost:8080` on that computer. Courts and phones on the same network open `http://<laptop address>:8080` instead, as [Your first tournament](../start-here/first-tournament.md) explains.

## Any computer with Docker

Docker runs the same app on macOS, Windows and Linux. Install [Docker Desktop](https://www.docker.com/products/docker-desktop/) (or Docker Engine on Linux). The tournament is stored in a `tournament-data` folder that you create once.

On Linux, make that folder writable by the container's user before the first run, or the app cannot save the tournament. Create it with `mkdir -p tournament-data`, then run `sudo chown 65534 tournament-data`. Docker Desktop on macOS and Windows handles that for you. [Install](install.md#docker) explains why, and lists the other images.

Then run the tournament app image:

```bash
mkdir -p tournament-data
docker run -p 8080:8080 -v "$PWD/tournament-data:/tournament-data" \
  ghcr.io/gitrgoliveira/bracket-creator-mobile:latest
```

In Windows PowerShell, type the `docker run` command on one line, without the `\`.

The image already listens for the courts and phones on the hall network.

## macOS: Homebrew

```bash
brew tap gitrgoliveira/tap
brew trust gitrgoliveira/tap
brew install bracket-creator
```

Homebrew builds the app on your Mac, so it needs Apple's command line developer tools. If you do not have them yet, `xcode-select --install` installs them. Then start the tournament app:

```bash
bracket-creator mobile-app --folder ./tournament-data --bind 0.0.0.0
```

`--bind 0.0.0.0` lets the courts and phones reach it; without it, only this Mac can.

## Linux: the package for your distribution

Each release on the [release page](https://github.com/gitrgoliveira/bracket-creator/releases/latest) carries a package for each Linux family, for amd64 (x86_64) and arm64 (aarch64). Download the one for your distribution and processor, then install it from the folder you saved it in:

=== "Debian, Ubuntu (.deb)"

    ```bash
    sudo apt install ./bracket-creator_*_$(dpkg --print-architecture).deb
    ```

=== "Fedora, RHEL (.rpm)"

    ```bash
    sudo dnf install ./bracket-creator-*.$(uname -m).rpm
    ```

=== "Alpine (.apk)"

    ```bash
    apk add --allow-untrusted ./bracket-creator_*_$(apk --print-arch).apk
    ```

    The package is not signed with an Alpine key, so it needs `--allow-untrusted`.

Then start the tournament app:

```bash
bracket-creator mobile-app --folder ./tournament-data --bind 0.0.0.0
```

The packages do not update themselves: [Upgrading](install.md#upgrading) says how to install a newer one.

## Windows: the release zip

Docker Desktop ([Any computer with Docker](#any-computer-with-docker)) also works on Windows. Without it, use the release zip:

1. Download the zip for your processor from the latest release:
    * Most Windows computers (Intel or AMD): [`bracket-creator_windows_x86_64.zip`](https://github.com/gitrgoliveira/bracket-creator/releases/latest/download/bracket-creator_windows_x86_64.zip)
    * Windows on Arm (for example a Snapdragon laptop): [`bracket-creator_windows_arm64.zip`](https://github.com/gitrgoliveira/bracket-creator/releases/latest/download/bracket-creator_windows_arm64.zip)

    Not sure? **Settings > System > About** shows the processor under **System type**.

2. Right-click the zip, choose **Extract All**, and extract it to a folder you will find again, for example `C:\bracket-creator`. It holds `bracket-creator.exe`.

3. Open that folder in File Explorer, click the address bar, type `powershell` and press Enter. A PowerShell window opens in the folder.

4. Start the tournament app:

    ```powershell
    .\bracket-creator.exe mobile-app --folder .\tournament-data --bind 0.0.0.0
    ```

    The tournament is stored in the `tournament-data` folder beside it. Keep the window open while the tournament runs; closing it stops the app.

5. When Windows asks whether to allow the app on networks, allow it on private networks, so the courts and phones can reach it.

The release files are not code-signed. If Windows blocks `bracket-creator.exe`, right-click the zip, choose **Properties**, tick **Unblock**, select **OK**, and extract it again.

To start it again on another day, repeat steps 3 and 4 in the same folder: the app finds the tournament where it left it.
