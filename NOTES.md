# Notes taken during the course of the project

## Getting the protocol with Wireshark and tcpdump

### tcpdump

Using the KEF Connect app on a Mac with Apple Silicon, I was able to capture the traffic between the app and the speaker. I used the following command to capture the traffic:

```bash
sudo tcpdump -s 0 -B8096 -w KEF.pcap -vvv host 10.0.0.93
```

### Wireshark

Opening the KEF.pcap file in Wireshark, filtering on `http` (Just typing http in the filter box) and then selecting the first packet, right click on it and select `Follow -> HTTP Stream` gives you a window with all HTTP requests and responses. This is a good way to get an overview of the protocol.
