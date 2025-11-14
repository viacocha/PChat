# PChat - Go-based Decentralized Secure Chat

## Overview

PChat is a decentralized secure messaging tool built using Go. It leverages P2P networking with end-to-end encryption (AES + RSA) to ensure privacy and security. There is no central server; users connect directly to each other, allowing for secure and anonymous communication.

## Features

- **P2P Network**: No centralized server, direct communication between users.
- **End-to-End Encryption**: Messages are encrypted with AES (symmetric encryption) and RSA (asymmetric encryption).
- **Anonymity**: Users don't need to register or use real names.
- **Offline Messaging**: Messages are encrypted and stored locally until the user comes online.
- **Message Self-Destruction**: Option to set message expiration time.

## Requirements

- Go 1.16 or higher
- Ubuntu or similar Linux distribution

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/viacocha/PChat.git
   cd PChat
