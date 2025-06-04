### Keeping Playlists in Sync

PlaylistPorter can detect and sync new tracks added to Spotify playlists after the initial porting:

```bash
# Check for new tracks and add them to the YouTube playlist
./bin/playlistporter -url "https://open.spotify.com/playlist/..." -sync
```

This is perfect for:
- Playlists that update regularly (Daily Mix, Discover Weekly)
- Collaborative playlists where friends add songs
- Your own playlists that you continue to update

**Automation Example:**
```bash
# Add to crontab for daily sync at 3 AM
0 3 * * * cd /path/to/playlistporter && ./bin/playlistporter -url "YOUR_PLAYLIST" -sync -max-tracks 20
```

See `CHECKPOINT_GUIDE.md` for detailed sync documentation and `scripts/sync-playlists.sh` for automation examples.