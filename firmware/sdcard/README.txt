lapcam SD card  -  https://github.com/uzulla/lapcam  (docs/sdcard.md)

This card installs lapcam on a Wyze Cam v3 and then stays in the camera:
the programs (lapcam, osd-feed, beacon, ffmpeg), the settings and the
recordings live here. The card must be FAT32.

INSTALL ON A STOCK WYZE CAM V3 (card made without -u)
  1. Camera unplugged. Insert this card. Plug the camera in.
  2. The LED cycles red / blue / purple while the stock firmware is backed up
     to WYZE_BACKUP_xxxxxx/ on this card and lapcam's firmware is written
     (a few minutes). The camera restarts by itself.
  3. First boot takes about a minute. With a wpa_supplicant.conf on this card
     the camera joins that Wi-Fi; open http://lapcam.local/ (or the camera's
     IP) and sign in as root with password "root". The app then asks you to
     set your own (Camera tab, "Change password"). Without Wi-Fi settings the camera opens its own network
     "lapcam-xxxx" (setup page at http://172.16.0.1/); hold the button 5 s any
     time to get an open network "lapcam" and reach http://100.64.1.1/.
  The installer renames itself to factory_t31_ZMC6tiIDQN.old when done, so
  the card will not install twice. To go back to Wyze: copy
  WYZE_BACKUP_xxxxxx/combined_backup.bin to the card as autoupdate-full.bin
  (alone on the card) and power the camera with it.

UPDATE A CAMERA ALREADY ON THINGINO / LAPCAM (card made with -u)
  autoupdate-full.bin on this card is written to the flash at the next power
  on (Thingino's bootloader does this; the red LED stays on while it writes,
  1-2 minutes, then the camera reboots), replacing everything, settings
  included - the settings on this card stay. autoupdate-full.done is left
  behind so it happens once; delete that file to flash again.

FILES
  thingino-wyze_cam3_*.bin / autoupdate-full.bin  lapcam's firmware image
  factory_t31_ZMC6tiIDQN      installer for the stock bootloader (thingino-installers)
  lapcam, osd-feed, beacon, ffmpeg   the programs (started from the card at boot)
  lapcam.args                 "-listen :80": lapcam takes port 80
  wpa_supplicant.conf         Wi-Fi networks, applied at boot (edit here or in lapcam's Wi-Fi tab)
  authorized_keys             SSH public keys, applied at boot (one per line; so ssh works after a flash)
  youtube-relay.json          stream key etc. (lapcam's Stream tab writes it)
  prudynt-osd.json            OSD layout (lapcam's OSD tab writes it)
  osd-feed.json               what the OSD shows (logo, G-ball, track, text)
  logo.png                    the logo shown by the OSD (the team's by default; replace it from lapcam's OSD tab)
  recordings/                 SD recordings, one folder per day
