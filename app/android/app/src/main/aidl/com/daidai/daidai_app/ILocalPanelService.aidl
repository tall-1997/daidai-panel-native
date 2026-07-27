package com.daidai.daidai_app;

interface ILocalPanelService {
    String ensureStarted();
    String status();
    String restart();
    String stop();
    String setPersistentSchedulingEnabled(boolean enabled);
}
