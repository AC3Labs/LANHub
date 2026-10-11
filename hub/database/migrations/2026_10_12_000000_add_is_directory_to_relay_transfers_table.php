<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Cross-machine transfers can now be whole folders, which RelayTransferJob
     * walks file by file instead of downloading a single file.
     */
    public function up(): void
    {
        Schema::table('relay_transfers', function (Blueprint $table) {
            $table->boolean('is_directory')->default(false);
        });
    }

    public function down(): void
    {
        Schema::table('relay_transfers', function (Blueprint $table) {
            $table->dropColumn('is_directory');
        });
    }
};
