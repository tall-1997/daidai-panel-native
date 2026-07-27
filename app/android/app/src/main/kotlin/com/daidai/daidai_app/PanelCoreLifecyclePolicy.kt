package com.daidai.daidai_app

object PanelCoreLifecyclePolicy {
    enum class Action { KEEP_RUNNING, STOP_CORE }

    fun onServiceDestroyed(): Action = Action.KEEP_RUNNING

    fun onPersistentDisabled(): Action = Action.KEEP_RUNNING

    fun onExplicitStop(): Action = Action.STOP_CORE
}
