<?php

use App\Models\Machine;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     *
     * A rolling history of free/total space per machine (from GET
     * /api/drives, summed across its drives) — enough to alert on a
     * transition into low-space and, later, chart a trend. Pruned by
     * the same command that inserts (see App\Console\Commands\
     * CheckDiskSpace), so this never grows unbounded.
     */
    public function up(): void
    {
        Schema::create('disk_readings', function (Blueprint $table) {
            $table->id();
            $table->foreignIdFor(Machine::class)->constrained()->cascadeOnDelete();
            $table->unsignedBigInteger('free_bytes');
            $table->unsignedBigInteger('total_bytes');
            $table->timestamp('recorded_at');
            $table->index(['machine_id', 'recorded_at']);
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('disk_readings');
    }
};
