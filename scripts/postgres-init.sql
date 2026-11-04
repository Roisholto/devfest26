-- Optional Docker-only initializer, executed once for a fresh named volume.
CREATE ROLE devfest26_presenter LOGIN;
CREATE ROLE devfest26_participant LOGIN;
CREATE DATABASE devfest26_presenter OWNER devfest26_presenter;
CREATE DATABASE devfest26_participant OWNER devfest26_participant;
