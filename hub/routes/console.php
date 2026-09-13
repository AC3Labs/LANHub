<?php

use Illuminate\Foundation\Inspiring;
use Illuminate\Support\Facades\Artisan;
use Illuminate\Support\Facades\Schedule;

Artisan::command('inspire', function () {
    $this->comment(Inspiring::quote());
})->purpose('Display an inspiring quote');

// Machine status previously only updated when someone had the Dashboard/
// Machines/Explorer page open in a browser — a machine going offline
// while nobody was looking stayed silently "online" in the DB until the
// next page load. This keeps status current and pushes a notification
// the moment a machine actually goes offline.
Schedule::command('machines:check-health')->everyMinute()->withoutOverlapping();

// Disk space changes slowly compared to online/offline status — every
// 15 minutes is frequent enough to catch a drive filling up without
// hammering every agent's /api/drives on every tick.
Schedule::command('machines:check-disk-space')->everyFifteenMinutes()->withoutOverlapping();

// Each SyncRule has its own interval_minutes and tracks its own
// last_run_at (see SyncRule::isDue()) — this just needs to tick often
// enough that a 1-minute rule can actually fire every minute.
Schedule::command('sync:run-rules')->everyMinute()->withoutOverlapping();
