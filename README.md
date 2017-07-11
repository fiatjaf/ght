This repository traffic statistics:

![](https://ght.trackingco.de/fiatjaf/ght?user=fiatjaf)

To showcase your repositories traffic statistics,

  1. visit https://ght.trackingco.de/_authorize and authorize access to your GitHub repositories
  2. add an image in your `README.md` with the following template:

```
[![](https://ght.trackingco.de/<your-username>/<your-repository-name>)](https://ght.trackingco.de/)
```

If you want to share statistics from a repository that you have access to, but it is not in your name, use the repository full name as above, but add `?user=<your-username>` so our server will know which access token it should use to fetch the statistics.

The traffic statistics are provided by the GitHub API and should be more-or-less the same as your `graphs/traffic` tab.

Please note that by authorizing access to one of your repositories, you'll also be giving access to anyone in the world to look at all your repositories traffic statistics (but not full read/write access to your repo, this is unthinkable), meaning that anyone can just visit `https://ght.trackingco.de/<your-name>/<a-different-repo-of-yours>` and see a graph of that repository traffic.
