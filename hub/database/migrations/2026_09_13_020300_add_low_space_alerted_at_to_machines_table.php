<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::table('machines', function (Blueprint $table) {
            // Set while a machine is below the low-space threshold, cleared
            // once it recovers — lets the disk-space check alert only on
            // the transition, the same pattern already used for
            // online/offline in App\Console\Commands\CheckMachineHealth.
            $table->timestamp('low_space_alerted_at')->nullable()->after('last_seen_at');
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('machines', function (Blueprint $table) {
            $table->dropColumn('low_space_alerted_at');
        });
    }
};
