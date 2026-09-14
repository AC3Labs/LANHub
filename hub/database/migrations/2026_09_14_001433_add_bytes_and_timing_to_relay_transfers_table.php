<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Adds what the per-machine Agents page needs to show real throughput
     * (sent/received totals, average speed): the actual byte count moved
     * (from the download's Content-Length, since a relay never sees the
     * destination write progress) and when the transfer actually started
     * moving bytes vs. just sat queued.
     */
    public function up(): void
    {
        Schema::table('relay_transfers', function (Blueprint $table) {
            $table->unsignedBigInteger('bytes_transferred')->nullable()->after('destination_path');
            $table->timestamp('started_at')->nullable()->after('status');
            $table->timestamp('completed_at')->nullable()->after('started_at');
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('relay_transfers', function (Blueprint $table) {
            $table->dropColumn(['bytes_transferred', 'started_at', 'completed_at']);
        });
    }
};
